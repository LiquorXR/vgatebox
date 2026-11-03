package main

import (
	"context"
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
)

type healthState int

const (
	healthOK healthState = iota
	healthTransient
	healthBlocked
)

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
	fails := map[int]int{}
	if !waitForWork(ctx, m.healthIntervalValue()) {
		return
	}
	for {
		m.healthRound(ctx, fails)
		if !waitForWork(ctx, m.healthIntervalValue()) {
			return
		}
	}
}

func (m *Manager) healthRound(ctx context.Context, fails map[int]int) {
	tunnels := m.Tunnels()
	active := make([]*Tunnel, 0, len(tunnels))
	for _, t := range tunnels {
		t.mu.Lock()
		status := t.Status
		t.mu.Unlock()
		if status == "up" {
			active = append(active, t)
		}
	}
	if len(active) == 0 {
		return
	}
	// Slots that left the round must not keep an old failure streak: a reused
	// slot would otherwise reconnect after a single ordinary failure.
	for slot := range fails {
		found := false
		for _, t := range active {
			if t.Slot == slot {
				found = true
				break
			}
		}
		if !found {
			delete(fails, slot)
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
	wg.Wait()
	close(results)

	// A cancelled round is shutdown, not evidence: incomplete probes must not
	// count toward the reconnect threshold or restart tunnels while stopping.
	if ctx.Err() != nil {
		return
	}
	for result := range results {
		t := result.tunnel
		switch result.state {
		case healthOK:
			fails[t.Slot] = 0
		case healthBlocked:
			fails[t.Slot] = 0
			log.Printf("隧道 %d (%s) 出口证据已失效，立即换节点重连", t.Slot, t.Node.HostName)
			m.reconnectHealth(t)
		case healthTransient:
			fails[t.Slot]++
			if fails[t.Slot] < healthFailures {
				log.Printf("隧道 %d (%s) 探测失败 %d 次", t.Slot, t.Node.HostName, fails[t.Slot])
				continue
			}
			log.Printf("隧道 %d (%s) 已掉线，正在换节点重连", t.Slot, t.Node.HostName)
			fails[t.Slot] = 0
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
	if view.Status != "up" {
		return healthOK, 0
	}
	t.opMu.Lock()
	defer t.opMu.Unlock()
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
			t.blockForPolicy("当前画像或本机 SOCKS 延迟不满足 Slot 策略")
			return healthBlocked, 0
		}
	}
	t.mu.Lock()
	t.LatencyMS = latency
	t.mu.Unlock()
	return healthOK, latency
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

// tunnelHealthy 保留给需要布尔结果的内部调用：任何非正常状态都视为不健康。
func (m *Manager) tunnelHealthy(t *Tunnel) bool {
	state, _ := m.checkTunnelHealth(context.Background(), t)
	return state == healthOK
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
	up := t.Status == "up" || t.Status == "blocked"
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
		defer t.opMu.Unlock()
		if ctx.Err() != nil || !m.tunnelActive(t) {
			return
		}
		bindingHost := t.prevHostOf()
		if t.Node.HostName != bindingHost {
			if err := m.rebind(bindingHost, t); err != nil {
				t.blockForPolicy("绑定配置应用失败，已阻断，请修复配置后重新启动 Slot")
				_ = m.saveState()
				log.Printf("重连后同步绑定失败: %v", err)
				return
			}
		} else if err := m.resync(t); err != nil {
			t.blockForPolicy("出站配置应用失败，已阻断，请修复配置后重新启动 Slot")
			_ = m.saveState()
			log.Printf("重连后重写出站失败: %v", err)
			return
		}
		t.setPrevHost("")
		if err := m.saveState(); err != nil {
			log.Printf("保存状态失败: %v", err)
		}
	}()
}
