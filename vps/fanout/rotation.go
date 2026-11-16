package main

import "log"

// rotationKind 区分换节点的来源。三种来源的校验与副作用不同：
//   - auto：健康重连/用户点“换 IP”，走 Swap（避开历史、清抖动计数）；
//   - target：用户指定节点，走 SwapTo（重新校验策略与占用）；
//   - performance：新出口仍慢时的有界再换一次，沿用 reconnectTo 的原始语义。
type rotationKind int

const (
	rotationAuto rotationKind = iota
	rotationTarget
	rotationPerformance
)

// rotationRequest 是一次排队的换节点意向。它由发布成功或性能比较产生，
// 由 tryCandidatesContext 退出 opMu 后交给 dispatchRotation 入队。
type rotationRequest struct {
	kind  rotationKind
	node  *Node
	cause string
}

type rotationJob struct {
	tunnel *Tunnel
	req    *rotationRequest
}

// dispatchRotation 是换节点请求的唯一入口。它只把请求放进单 worker 队列，
// 不直接调用 Swap/SwapTo/reconnectTo；worker 边界切断了运行期的直接回调，
// 也把跨 Slot 的换节点动作串行化。生产在 initControl 里启动 worker。
func (m *Manager) dispatchRotation(t *Tunnel, req *rotationRequest) {
	if req == nil {
		return
	}
	m.rotationCh <- rotationJob{tunnel: t, req: req}
}

// startRotationWorker 由生产启动流程调用一次；测试需要时也可显式调用。
// 不放在 NewManager 里，避免诊断探测的临时 Manager 每次泄漏一个常驻 goroutine。
func (m *Manager) startRotationWorker() {
	m.rotationOnce.Do(func() {
		go m.rotationWorker()
	})
}

func (m *Manager) rotationWorker() {
	for job := range m.rotationCh {
		m.executeRotation(job.tunnel, job.req)
	}
}

// executeRotation 只在 rotationWorker 里调用。执行前重新检查 Slot 是否仍
// 存在/未停止，避免对已删除或已 Stop 的隧道操作。
func (m *Manager) executeRotation(t *Tunnel, req *rotationRequest) {
	if req == nil || !m.tunnelActive(t) {
		return
	}
	if m.rotationHook != nil {
		m.rotationHook(t, req)
		return
	}
	var err error
	switch req.kind {
	case rotationPerformance:
		if req.node == nil {
			return
		}
		t.changeMu.Lock()
		defer t.changeMu.Unlock()
		if !m.tunnelActive(t) {
			return
		}
		t.mu.Lock()
		stopped := t.Status == "stopped"
		oldHost := t.Node.HostName
		t.mu.Unlock()
		if stopped {
			return
		}
		t.rememberSwap(oldHost)
		m.reconnectTo(t, oldHost, req.node, req.cause)
	case rotationTarget:
		if req.node == nil {
			return
		}
		err = m.SwapTo(t.Slot, req.node.HostName)
	default:
		err = m.Swap(t.Slot)
	}
	if err != nil {
		log.Printf("隧道 %d 排队换节点未执行（%s）: %v", t.Slot, req.cause, err)
	}
}

// takePendingRotation 原子取出排队意向。调用方通常在退出 opMu 之后调用，
// 保证 dispatchRotation 不会在持锁路径里触发换节点。
func (t *Tunnel) takePendingRotation() *rotationRequest {
	t.mu.Lock()
	defer t.mu.Unlock()
	req := t.pendingRotation
	t.pendingRotation = nil
	return req
}

func (t *Tunnel) clearPendingRotation() {
	t.mu.Lock()
	t.pendingRotation = nil
	t.mu.Unlock()
}
