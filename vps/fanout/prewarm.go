package main

// prewarm.go：degraded 出口的后台热备（预验）与性能切前比对。
//
// 热备只回答"哪个候选大概率能连上"，不跳过生产建链的完整验证：
// 重连时优先试预验通过的节点，省掉对一批死节点逐个 40s 握手。
// 预验走诊断身份（独立 netns/Slot），与生产抢 connectBudget 时生产优先
// （见 connectBudget.acquire 的 production 语义），开销有界：
// 每 Slot 最多一路在飞，结果 5 分钟有效，手动操作/建连成功即清。
import (
	"context"
	"log"
	"time"
)

const (
	// prewarmTTL 是热备结果的有效期：志愿者节点 churn 大，过期即弃。
	prewarmTTL = 5 * time.Minute
	// perfWorseRatio 是切前比对阈值：新节点平滑延迟超过换前该倍数，
	// 则有界再换一次。1.5 兼顾毛刺容忍与真换差。
	perfWorseRatio = 1.5
	// cooldownBase/cooldownMax 是候选失败退避：30s 起步，指数到 120s 封顶。
	cooldownBase = 30 * time.Second
	cooldownMax  = 120 * time.Second
)

// prewarmFresh 返回新鲜的热备候选（副本），过期或无结果返回 nil。
func (t *Tunnel) prewarmFresh() *Node {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.prewarmNode == nil || time.Since(t.prewarmAt) > prewarmTTL {
		return nil
	}
	cp := *t.prewarmNode
	return &cp
}

func (t *Tunnel) setPrewarm(n Node) {
	t.mu.Lock()
	t.prewarmNode, t.prewarmAt = &n, time.Now()
	t.mu.Unlock()
}

func (t *Tunnel) clearPrewarm() {
	t.mu.Lock()
	t.prewarmNode, t.prewarming = nil, false
	t.mu.Unlock()
}

// takePrewarming 抢占唯一的后台预验名额，抢到返回 true。
func (t *Tunnel) takePrewarming() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.prewarming {
		return false
	}
	if t.prewarmNode != nil && time.Since(t.prewarmAt) <= prewarmTTL {
		return false
	}
	t.prewarming = true
	return true
}

func (t *Tunnel) releasePrewarming() {
	t.mu.Lock()
	t.prewarming = false
	t.mu.Unlock()
}

func (t *Tunnel) setPerfFloor(v float64) {
	t.mu.Lock()
	t.perfFloor = v
	t.mu.Unlock()
}

// takePerfFloor 取出并清掉性能基线，用于切后比对（单次生效）。
func (t *Tunnel) takePerfFloor() (float64, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	floor, ok := t.perfFloor, t.perfFloor > 0
	t.perfFloor = 0
	return floor, ok
}

// clearSpeculation 清掉后台推测状态（热备、性能基线）。手动 Resume/Swap/
// 改策略时调用：operator 意图优先，旧推测全部作废。
func (t *Tunnel) clearSpeculation() {
	t.mu.Lock()
	t.prewarmNode, t.prewarming, t.perfFloor = nil, false, 0
	t.mu.Unlock()
}

// noteCandidateFailed 记录一次候选失败并设指数退避：30s→60s→120s封顶。
// 调用方仍需自行设置 t.Err。
func (t *Tunnel) noteCandidateFailed(host string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.cooldown == nil {
		t.cooldown = map[string]time.Time{}
	}
	if t.failCounts == nil {
		t.failCounts = map[string]int{}
	}
	t.failCounts[host]++
	shift := t.failCounts[host] - 1
	if shift > 2 {
		shift = 2
	}
	t.cooldown[host] = time.Now().Add(cooldownBase << shift)
}

// shouldRotateForPerf 是切前比对谓词：新节点比换前慢过阈值则再换一次。
func shouldRotateForPerf(floor, smooth float64) bool {
	return floor > 0 && smooth > floor*perfWorseRatio
}

// maybePrewarm 在隧道刚进入 degraded 时触发一次后台预验。
// 非阻塞：抢不到名额、无候选、无 Console（单测裸 Manager）直接返回。
func (m *Manager) maybePrewarm(t *Tunnel) {
	if m.console == nil || !t.takePrewarming() {
		return
	}
	picks := m.policyCandidates(t.policySnapshot(), t.swapAvoid(), t.Slot)
	if len(picks) == 0 {
		t.releasePrewarming()
		return
	}
	go m.runPrewarm(t, picks[0])
}

func (m *Manager) runPrewarm(t *Tunnel, node Node) {
	defer t.releasePrewarming()
	c := m.console
	if c == nil {
		return
	}
	if !m.tunnelActive(t) {
		return
	}
	ctx, cancel := context.WithTimeout(c.ctx, 3*time.Minute)
	defer cancel()
	result := c.probe(ctx, node, diagnosticFirstSlot, func(string) {})
	if result.Status != "passed" {
		return
	}
	t.mu.Lock()
	stillDegraded := t.Status == "degraded"
	t.mu.Unlock()
	if !stillDegraded || !m.tunnelActive(t) || !m.prewarmStillValid(node) {
		return
	}
	t.setPrewarm(node)
	log.Printf("隧道 %d 热备候选 %s 预验通过", t.Slot, node.HostName)
}

// prewarmStillValid 核对热备目标仍在当前池中且配置未变。
func (m *Manager) prewarmStillValid(node Node) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, n := range m.nodes {
		if n.HostName == node.HostName {
			return n.Config == node.Config
		}
	}
	return false
}

// rotateIfPerfWorse 在性能触发的重连成功后比对：新节点比换前慢过阈值
// 则有界再换一次（基线单次生效，不连环）。在 reconnectTo 收尾处调用，
// 此时已释放 changeMu，与手动操作互斥串行。
func (m *Manager) rotateIfPerfWorse(t *Tunnel) {
	floor, ok := t.takePerfFloor()
	if !ok {
		return
	}
	if !shouldRotateForPerf(floor, t.smoothLatency()) {
		return
	}
	node, err := m.pickSwapTarget(t)
	if err != nil {
		log.Printf("隧道 %d 新出口仍慢但无替代候选，保持现状", t.Slot)
		return
	}
	log.Printf("隧道 %d 新出口延迟 %.0fms 差于换前 %.0fms，再换到 %s",
		t.Slot, t.smoothLatency(), floor, node.HostName)
	oldHost := t.Node.HostName
	t.rememberSwap(oldHost)
	m.reconnectTo(t, oldHost, &node)
}
