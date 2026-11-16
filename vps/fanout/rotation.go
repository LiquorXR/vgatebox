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
// 由 tryCandidatesContext 退出 opMu 后统一交给 dispatchRotation 执行。
type rotationRequest struct {
	kind  rotationKind
	node  *Node
	cause string
}

// dispatchRotation 是换节点请求的唯一异步入口。它只在 opMu 之外调用，
// 把 publishUp / rotateIfPerfWorse 的回调集中到一处；调用环仍存在
// （B2b 用单 worker 队列断开），但不再由发布路径直接递归换节点。
func (m *Manager) dispatchRotation(t *Tunnel, req *rotationRequest) {
	if req == nil {
		return
	}
	go func() {
		if !m.tunnelActive(t) {
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
	}()
}

// takePendingRotation 原子取出排队意向。调用方通常在退出 opMu 之后调用，
// 保证 dispatchRotation 不会在持锁路径里回调 reconnectTo。
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
