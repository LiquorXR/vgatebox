package main

import (
	"context"
	"fmt"
	"net"
	"reflect"
	"time"
)

// opMu serializes a Slot's network operations. Cancellation gates new traffic
// immediately, while commands unwind before the next operation can touch netns.
func (t *Tunnel) workContext() context.Context {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.workCtx == nil {
		return context.Background()
	}
	return t.workCtx
}

func (t *Tunnel) resetWork() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.workCtx, t.cancelWork = context.WithCancel(context.Background())
}

func (t *Tunnel) blockWork() {
	t.mu.Lock()
	if t.cancelWork != nil {
		t.cancelWork()
	}
	t.validating = false
	if t.Status != "stopped" {
		t.Status = "starting"
	}
	clients := make([]net.Conn, 0, len(t.clients))
	for c := range t.clients {
		clients = append(clients, c)
	}
	t.mu.Unlock()
	for _, c := range clients {
		_ = c.Close()
	}
}

func (t *Tunnel) blockForPolicy(reason string) {
	t.blockWork()
	t.mu.Lock()
	if t.Status != "stopped" {
		t.Status, t.Err = "blocked", reason
	}
	t.mu.Unlock()
}

// markDegraded 把隧道标记为"变慢但不断流"：只改状态与原因，
// 不取消握手上下文、不关已建立连接、不动监听，新连接继续按当前凭据服务。
// 只有 up/degraded 之间流转；stopped/starting/waiting 的隧道不适用。
func (t *Tunnel) markDegraded(reason string) {
	t.mu.Lock()
	if t.Status == "up" || t.Status == "degraded" {
		t.Status, t.Err = "degraded", reason
	}
	t.mu.Unlock()
}

// absorbLatency 把一次探活延迟吸收进 EWMA 平滑值，不改状态。
// 首个样本（平滑值<=0）直接采用，用于建连初始化。
func (t *Tunnel) absorbLatency(latency float64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.latencyInstantMS = latency
	if t.LatencyMS <= 0 {
		t.LatencyMS = latency
		return
	}
	t.LatencyMS = ewmaAlpha*latency + (1-ewmaAlpha)*t.LatencyMS
}

// smoothLatency 读 EWMA 平滑延迟（展示、排序与门限口径）。
func (t *Tunnel) smoothLatency() float64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.LatencyMS
}

// markRecovered 把 degraded 恢复为 up，同时吸收本次延迟。
func (t *Tunnel) markRecovered(latency float64) {
	t.absorbLatency(latency)
	t.mu.Lock()
	if t.Status == "degraded" {
		t.Status, t.Err = "up", ""
		t.probeStage, t.probeStageAt = "", time.Time{}
	}
	t.mu.Unlock()
}

// queuePendingSwap 记录排队意向：to!=nil 为 SwapTo 指定节点，auto 为 Swap。
// 调用方（starting 中）不再报错，发布成功后由 swapPending 异步执行。
func (t *Tunnel) queuePendingSwap(node *Node, auto bool) {
	t.mu.Lock()
	t.pendingSwapTo, t.pendingSwapAuto = node, auto
	t.mu.Unlock()
}

func (t *Tunnel) clearPendingSwap() {
	t.mu.Lock()
	t.pendingSwapTo, t.pendingSwapAuto = nil, false
	t.mu.Unlock()
}

func (t *Tunnel) hasPendingSwap() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.pendingSwapAuto || t.pendingSwapTo != nil
}

// takePendingSwap 原子取出排队意向：node==nil 且 auto 为真表示自动挑。
// publishUp 内因已持 t.mu 而内联此逻辑，此处供其他调用方。
func (t *Tunnel) takePendingSwap() (node *Node, auto, ok bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.pendingSwapAuto && t.pendingSwapTo == nil {
		return nil, false, false
	}
	node, auto = t.pendingSwapTo, t.pendingSwapAuto
	t.pendingSwapTo, t.pendingSwapAuto = nil, false
	return node, auto, true
}

func (m *Manager) claimNode(t *Tunnel, node Node, ctx context.Context) bool {
	for {
		m.mu.Lock()
		if m.tunnels[t.Slot] != t || ctx.Err() != nil {
			m.mu.Unlock()
			return false
		}
		lease := m.diagnostics[node.HostName]
		if lease == nil {
			for _, other := range m.diagnostics {
				if (node.IP != "" && other.ip == node.IP) || other.networkIndex == slotNetworkIndex(t.Slot) {
					lease = other
					break
				}
			}
		}
		if lease == nil {
			break // Keep the lock through the final claim below.
		}
		lease.cancel(errDiagnosticPreempted)
		m.mu.Unlock()
		select {
		case <-lease.done:
		case <-ctx.Done():
			return false
		}
	}
	defer m.mu.Unlock()
	if m.tunnels[t.Slot] != t || ctx.Err() != nil {
		return false
	}
	for _, other := range m.tunnels {
		if other == t {
			continue
		}
		other.mu.Lock()
		occupied := other.Status != "stopped" && other.Node.HostName == node.HostName
		other.mu.Unlock()
		if occupied {
			return false
		}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.Status == "stopped" || ctx.Err() != nil {
		return false
	}
	t.Node, t.Status = node, "starting"
	return true
}

func (m *Manager) publishUp(t *Tunnel, ctx context.Context) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.tunnels[t.Slot] != t || ctx.Err() != nil {
		return false
	}
	t.mu.Lock()
	ip := t.ExitIP
	t.mu.Unlock()
	for _, other := range m.tunnels {
		if other == t {
			continue
		}
		other.mu.Lock()
		duplicate := other.Status == "up" && ip != "" && other.ExitIP == ip
		other.mu.Unlock()
		if duplicate {
			return false
		}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if ctx.Err() != nil || t.workCtx != ctx || t.Status == "stopped" || t.egress == nil {
		return false
	}
	t.Status, t.Err = "up", ""
	// 建连成功：热备已消费，失败计数清零；排队意向原子取出后异步执行
	// （调用方持有 opMu，同步调 reconnectTo 会自死锁）。
	// 阶段一并清空：“查询出口画像”只是最后一站的残留，留着误导。
	t.probeStage, t.probeStageAt = "", time.Time{}
	t.prewarmNode, t.failCounts = nil, nil
	pendingNode, pendingAuto := t.pendingSwapTo, t.pendingSwapAuto
	t.pendingSwapTo, t.pendingSwapAuto = nil, false
	if pendingAuto || pendingNode != nil {
		go m.swapPending(t, pendingNode, pendingAuto)
	}
	return true
}

func waitForWork(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (m *Manager) SetSlotPolicy(slot int, p SlotPolicy) error {
	normalized, err := normalizePolicy(p)
	if err != nil {
		return err
	}
	m.mu.RLock()
	t := m.tunnels[slot]
	m.mu.RUnlock()
	if t == nil {
		return fmt.Errorf("Slot 不存在")
	}
	t.changeMu.Lock()
	defer t.changeMu.Unlock()
	return m.setSlotPolicyLocked(t, normalized)
}

func (m *Manager) setSlotPolicyLocked(t *Tunnel, normalized SlotPolicy) error {
	if !m.ownsTunnel(t) {
		return fmt.Errorf("Slot 已删除")
	}
	// Block before waiting on an in-flight handshake/query; cancellation kills
	// its child command and prevents it from publishing up under an old policy.
	t.mu.Lock()
	stopped := t.Status == "stopped"
	t.mu.Unlock()
	t.blockWork()
	t.opMu.Lock()
	old := t.policySnapshot()
	t.mu.Lock()
	oldPrevious := t.previousPolicy
	if !reflect.DeepEqual(old, normalized) {
		t.previousPolicy = &old
	}
	t.Policy = &normalized
	t.mu.Unlock()
	if err := m.saveState(); err != nil {
		t.mu.Lock()
		t.Policy = &old
		t.previousPolicy = oldPrevious
		if stopped {
			t.Status = "stopped"
		} else {
			t.Status = "blocked"
		}
		t.Err = "策略保存失败，Slot 保持阻断"
		t.mu.Unlock()
		t.opMu.Unlock()
		return err
	}
	if stopped {
		t.opMu.Unlock()
		return nil
	}
	oldHost := t.Node.HostName
	t.opMu.Unlock()
	t.clearFlap()
	t.clearSpeculation()
	m.reconnectTo(t, oldHost, nil, "策略已更新，正在按新策略重新验活")
	return nil
}

func (m *Manager) Resume(slot int) error {
	m.mu.RLock()
	t := m.tunnels[slot]
	m.mu.RUnlock()
	if t == nil {
		return fmt.Errorf("Slot 不存在")
	}
	t.changeMu.Lock()
	defer t.changeMu.Unlock()
	if !m.ownsTunnel(t) {
		return fmt.Errorf("Slot 已删除")
	}
	t.mu.Lock()
	stopped := t.Status == "stopped" || t.Status == "blocked" || t.Status == "waiting_for_nodes" || t.Status == "failed"
	t.mu.Unlock()
	if !stopped {
		return fmt.Errorf("Slot 已在运行或连接中")
	}
	t.mu.Lock()
	t.Status = "starting"
	t.mu.Unlock()
	t.clearFlap()
	t.clearSpeculation()
	m.reconnectTo(t, t.Node.HostName, nil, "手动启动，正在重新连接")
	return nil
}

func (m *Manager) RollbackSlotPolicy(slot int) error {
	m.mu.RLock()
	t := m.tunnels[slot]
	m.mu.RUnlock()
	if t == nil {
		return fmt.Errorf("Slot 不存在")
	}
	t.changeMu.Lock()
	defer t.changeMu.Unlock()
	t.mu.Lock()
	before := t.previousPolicy
	t.mu.Unlock()
	if before == nil {
		return fmt.Errorf("没有上一次 Slot 策略")
	}
	normalized, err := normalizePolicy(*before)
	if err != nil {
		return err
	}
	return m.setSlotPolicyLocked(t, normalized)
}
