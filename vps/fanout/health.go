package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

const (
	healthInterval  = 10 * time.Second
	healthFailures  = 2 // 连续失败几次才判定掉线，避免网络抖动误杀
	healthProbeTime = 10 * time.Second
	healthEchoTime  = 8 * time.Second
	healthParallel  = 8
	// A busy Slot (manual swap, console exit check) must not hold a probe permit
	// for the whole operation: that would stretch the round for every other exit.
	healthOpWait = 250 * time.Millisecond
	// healthDegradedRounds 是变慢升级为重连的宽限：连续 6 轮（约 60s）延迟
	// 超限才换节点，单次毛刺只标记观察、不断流。
	healthDegradedRounds = 6
)

type healthState int

const (
	healthOK healthState = iota
	healthTransient
	healthBlocked
	// healthDeferred means the round could not observe the tunnel because
	// another network operation owned it. It must change neither the streak nor
	// the tunnel state.
	healthDeferred
	// healthDegraded means the tunnel still serves traffic but its latency
	// exceeds the Slot policy. It counts toward a separate upgrade streak
	// instead of an immediate reconnect.
	healthDegraded
)

type healthRoundState struct {
	fails    map[int]int
	deferred map[int]bool
	degraded map[int]int
}

func newHealthRoundState() *healthRoundState {
	return &healthRoundState{fails: map[int]int{}, deferred: map[int]bool{}, degraded: map[int]int{}}
}

type healthRoundResult struct {
	tunnel *Tunnel
	state  healthState
}

// WatchHealth 周期检查每条隧道是否还能出网，掉线的自动换节点重连。
// VPN Gate 是志愿者节点，运行中掉线很常见。
//
// A round starts after the interval, probes every up tunnel concurrently, and
// only then waits for the next interval. There is no tick backlog or overlap;
// cancellation stops the wait and the in-flight probes.
func (m *Manager) WatchHealth(ctx context.Context) {
	state := newHealthRoundState()
	if !waitForWork(ctx, m.healthIntervalValue()) {
		return
	}
	for {
		m.healthRound(ctx, state)
		if !waitForWork(ctx, m.healthIntervalValue()) {
			return
		}
	}
}

func (m *Manager) healthRound(ctx context.Context, state *healthRoundState) {
	tunnels := m.Tunnels()
	active := make([]*Tunnel, 0, len(tunnels))
	for _, t := range tunnels {
		t.mu.Lock()
		status := t.Status
		t.mu.Unlock()
		if servingStatus(status) {
			active = append(active, t)
		}
	}
	if len(active) == 0 {
		return
	}
	// Slots that left the round must not keep an old failure streak: a reused
	// slot would otherwise reconnect after a single ordinary failure.
	// degraded 计数同理：复用的 Slot 不应继承上一次的变慢宽限。
	left := map[int]bool{}
	for slot := range state.fails {
		left[slot] = true
	}
	for slot := range state.degraded {
		left[slot] = true
	}
	for slot := range left {
		found := false
		for _, t := range active {
			if t.Slot == slot {
				found = true
				break
			}
		}
		if !found {
			delete(state.fails, slot)
			delete(state.deferred, slot)
			delete(state.degraded, slot)
		}
	}
	parallel := m.healthParallelValue()
	sem := make(chan struct{}, parallel)
	results := make(chan healthRoundResult, len(active))
	var wg sync.WaitGroup
	for _, t := range active {
		wg.Add(1)
		go func(t *Tunnel) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				results <- healthRoundResult{tunnel: t, state: healthTransient}
				return
			}
			defer func() { <-sem }()
			state, _ := m.checkTunnelHealth(ctx, t)
			results <- healthRoundResult{tunnel: t, state: state}
		}(t)
	}
	// 先关通道再消费：先到的结果先决策，慢隧道不再拖住已确认掉线的重连。
	// results 带 len(active) 缓冲，提前返回也不会卡住 worker。
	go func() {
		wg.Wait()
		close(results)
	}()
	for result := range results {
		// A cancelled round is shutdown, not evidence: decisions that arrive
		// after cancellation are skipped instead of counting toward reconnects.
		if ctx.Err() != nil {
			continue
		}
		t := result.tunnel
		switch result.state {
		case healthOK:
			state.fails[t.Slot] = 0
			state.deferred[t.Slot] = false
			state.degraded[t.Slot] = 0
		case healthBlocked:
			state.fails[t.Slot] = 0
			state.deferred[t.Slot] = false
			state.degraded[t.Slot] = 0
			log.Printf("隧道 %d (%s) 出口证据已失效，立即换节点重连", t.Slot, t.Node.HostName)
			m.reconnectHealth(t)
		case healthDegraded:
			state.fails[t.Slot] = 0
			state.deferred[t.Slot] = false
			state.degraded[t.Slot]++
			if state.degraded[t.Slot] < healthDegradedRounds {
				if state.degraded[t.Slot] == 1 {
					log.Printf("隧道 %d (%s) 延迟超限，仍可出网并观察中", t.Slot, t.Node.HostName)
					m.maybePrewarm(t)
				}
				continue
			}
			log.Printf("隧道 %d (%s) 持续变慢，正在换节点重连", t.Slot, t.Node.HostName)
			state.degraded[t.Slot] = 0
			// 记下换前平滑延迟供切后比对，并把慢节点记入避让史，
			// 本轮重连与后续手动换节点都不再优先重试它。
			t.setPerfFloor(t.smoothLatency())
			t.rememberSwap(t.Node.HostName)
			m.reconnectHealth(t)
		case healthDeferred:
			// A busy Slot is not evidence of failure, but it must not look
			// healthy either: leave the streak untouched and retry next round.
			if !state.deferred[t.Slot] {
				log.Printf("隧道 %d (%s) 正被其它操作占用，本轮跳过健康检查", t.Slot, t.Node.HostName)
				state.deferred[t.Slot] = true
			}
		case healthTransient:
			state.fails[t.Slot]++
			if state.fails[t.Slot] < healthFailures {
				log.Printf("隧道 %d (%s) 探测失败 %d 次", t.Slot, t.Node.HostName, state.fails[t.Slot])
				continue
			}
			log.Printf("隧道 %d (%s) 已掉线，正在换节点重连", t.Slot, t.Node.HostName)
			state.fails[t.Slot] = 0
			m.reconnectHealth(t)
		}
	}
}

func (m *Manager) reconnectHealth(t *Tunnel) {
	if m.healthReconnect != nil {
		m.healthReconnect(t, t.Node.HostName)
		return
	}
	m.reconnect(t, t.Node.HostName)
}

func (m *Manager) checkTunnelHealth(parent context.Context, t *Tunnel) (healthState, float64) {
	view := t.snapshot()
	if view.Status == "blocked" {
		t.blockForPolicy("画像证据已过期或不满足当前策略")
		return healthBlocked, 0
	}
	if !servingStatus(view.Status) {
		return healthOK, 0
	}
	// Never wait unbounded for the Slot's operation lock: a manual swap or a
	// console exit check can hold it for tens of seconds, and this goroutine is
	// occupying a probe permit while it waits.
	if !lockOpWithin(parent, &t.opMu, m.healthOpWaitValue()) {
		return healthDeferred, 0
	}
	defer t.opMu.Unlock()
	// The snapshot above was taken before the lock; re-read the state so a Slot
	// that started reconnecting meanwhile is not judged against old evidence.
	switch t.snapshot().Status {
	case "up", "degraded":
	case "blocked":
		return healthBlocked, 0
	default:
		return healthOK, 0
	}
	if parent.Err() != nil {
		return healthTransient, 0
	}
	// A stale profile or a policy change invalidates the exit immediately; it
	// must not be hidden behind the ordinary two-failure tolerance.
	if t.Policy != nil {
		image := t.egressSnapshot(false)
		if image == nil || t.policySnapshot().checkImage(*image) != nil {
			t.blockForPolicy("当前画像或策略证据已失效")
			return healthBlocked, 0
		}
	}
	probeCtx, cancel := context.WithTimeout(parent, m.healthProbeTimeValue())
	defer cancel()
	got, latency, err := m.probeTunnelHealth(probeCtx, t)
	if err != nil {
		return healthTransient, 0
	}
	if probeCtx.Err() != nil {
		return healthTransient, 0
	}
	// 出口 IP 变了说明 VPN 已经断开，流量退回了母机
	if got != t.ExitIP {
		t.blockForPolicy("实际出口 IP 已变化，必须重新建立基线")
		return healthBlocked, 0
	}
	if t.Policy != nil {
		if err := t.policySnapshot().checkLatency(latency); err != nil {
			// 单采超限先看吸收前的平滑值：毛刺只吸收进 EWMA，不断流不标记；
			// 平滑值同样超限才是真慢，标记观察。注意先裁决后吸收，
			// 否则一次大毛刺就会把平滑值冲过门限、复判恒败。
			prior := t.smoothLatency()
			t.absorbLatency(latency)
			if terr := t.policySnapshot().checkLatency(prior); terr != nil {
				t.markDegraded("出口延迟超过 Slot 上限，仍可出网并观察中")
				return healthDegraded, 0
			}
			return healthOK, latency
		}
	}
	t.markRecovered(latency)
	return healthOK, latency
}

// lockOpWithin takes the Slot's operation lock without waiting for a long
// connect or exit check. Probes must give up and try again next round instead
// of holding a permit; otherwise one busy Slot delays every other exit.
func lockOpWithin(ctx context.Context, mu *sync.Mutex, budget time.Duration) bool {
	deadline := time.Now().Add(budget)
	for {
		if mu.TryLock() {
			return true
		}
		if ctx.Err() != nil || !time.Now().Before(deadline) {
			return false
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return false
		case <-timer.C:
		}
	}
}

func (m *Manager) probeTunnelHealth(ctx context.Context, t *Tunnel) (string, float64, error) {
	if m.healthProbe != nil {
		return m.healthProbe(ctx, t)
	}
	return t.probeSocksHealth(ctx, m.healthEchoTimeValue())
}

func (m *Manager) healthIntervalValue() time.Duration {
	if m.healthEvery > 0 {
		return m.healthEvery
	}
	return healthInterval
}

func (m *Manager) healthProbeTimeValue() time.Duration {
	if m.healthProbeWindow > 0 {
		return m.healthProbeWindow
	}
	return healthProbeTime
}

func (m *Manager) healthEchoTimeValue() time.Duration {
	if m.healthEchoWindow > 0 {
		return m.healthEchoWindow
	}
	return healthEchoTime
}

func (m *Manager) healthParallelValue() int {
	if m.healthParallelLimit > 0 {
		return m.healthParallelLimit
	}
	return healthParallel
}

func (m *Manager) healthOpWaitValue() time.Duration {
	if m.healthOpWindow > 0 {
		return m.healthOpWindow
	}
	return healthOpWait
}

// reconnect 就地把一条隧道换到别的节点上，保持槽位与端口不变，
// 这样已经分发出去的客户端配置仍然可用。
//
// oldHost 必须是本次重连前那条隧道真正绑着的节点名。调用方若已经
// 改过 t.Node（比如手动换节点），就要把改之前的名字传进来，
// 否则 rebind 找不到旧绑定，入站会掉成孤儿。
func (m *Manager) reconnect(t *Tunnel, oldHost string) {
	t.changeMu.Lock()
	defer t.changeMu.Unlock()
	t.mu.Lock()
	up := t.Status == "up" || t.Status == "degraded" || t.Status == "blocked"
	t.mu.Unlock()
	if !up {
		return
	}
	m.reconnectTo(t, oldHost, nil)
}

func (m *Manager) reconnectTo(t *Tunnel, oldHost string, node *Node) {
	t.blockWork()
	t.opMu.Lock()
	if !m.tunnelActive(t) {
		t.opMu.Unlock()
		return
	}
	t.mu.Lock()
	t.Status, t.Err, t.ExitIP = "starting", "正在按 Slot 策略重新验活", ""
	if node != nil {
		t.Node = *node
	}
	if t.prevHost == "" {
		t.prevHost = oldHost
	}
	t.mu.Unlock()
	if recent := t.recordReconnect(); recent >= flapThreshold {
		t.mu.Lock()
		t.Err = fmt.Sprintf("频繁掉线（10分钟内第 %d 次），已降频重试", recent)
		t.mu.Unlock()
		log.Printf("隧道 %d 频繁重连，已降频重试", t.Slot)
	}
	if err := m.saveState(); err != nil {
		log.Printf("保存状态失败: %v", err)
	}
	t.teardownNetns()
	t.resetWork()
	ctx := t.workContext()
	t.opMu.Unlock()
	go func() {
		m.bringUpPersistContext(t, false, true, ctx)
		if ctx.Err() != nil || t.Status != "up" || !m.tunnelActive(t) {
			return
		}
		t.opMu.Lock()
		if ctx.Err() != nil || !m.tunnelActive(t) {
			t.opMu.Unlock()
			return
		}
		bindingHost := t.prevHostOf()
		if t.Node.HostName != bindingHost {
			if err := m.rebind(bindingHost, t); err != nil {
				t.blockForPolicy("绑定配置应用失败，已阻断，请修复配置后重新启动 Slot")
				_ = m.saveState()
				t.opMu.Unlock()
				log.Printf("重连后同步绑定失败: %v", err)
				return
			}
		} else if err := m.resync(t); err != nil {
			t.blockForPolicy("出站配置应用失败，已阻断，请修复配置后重新启动 Slot")
			_ = m.saveState()
			t.opMu.Unlock()
			log.Printf("重连后重写出站失败: %v", err)
			return
		}
		t.setPrevHost("")
		if err := m.saveState(); err != nil {
			log.Printf("保存状态失败: %v", err)
		}
		t.opMu.Unlock()
		// 性能触发的重连成功后比对：新节点比换前慢过阈值则有界再换一次。
		// 基线单次生效，手动操作会提前清掉，不连环。必须在 opMu 之外调，
		// reconnectTo 内部要拿同一把锁。
		m.rotateIfPerfWorse(t)
	}()
}
