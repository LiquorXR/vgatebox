package main

import "log/slog"

// slotPhase 是 Tunnel.Status 的类型化视图。持久化格式仍是 state.json 里的
// 原字符串，因此常量值必须与旧值逐字一致，不能改名。
type slotPhase string

const (
	slotStarting        slotPhase = "starting"
	slotUp              slotPhase = "up"
	slotDegraded        slotPhase = "degraded"
	slotBlocked         slotPhase = "blocked"
	slotWaitingForNodes slotPhase = "waiting_for_nodes"
	slotFailed          slotPhase = "failed"
	slotStopped         slotPhase = "stopped"
)

// serving 与 servingStatus 保持同一语义：up 和 degraded 都接受新连接。
func (p slotPhase) serving() bool { return p == slotUp || p == slotDegraded }

// slotPhaseTransitions 是允许的状态转换表。空字符串表示未初始化的 Tunnel
// （测试夹具或构造中的对象），允许进入任意状态。
var slotPhaseTransitions = map[slotPhase]map[slotPhase]bool{
	slotStarting: {
		slotUp:              true,
		slotDegraded:        true,
		slotBlocked:         true,
		slotWaitingForNodes: true,
		slotFailed:          true,
		slotStopped:         true,
	},
	slotUp: {
		slotUp:       true,
		slotDegraded: true,
		slotStarting: true,
		slotBlocked:  true,
		slotStopped:  true,
	},
	slotDegraded: {
		slotUp:       true,
		slotStarting: true,
		slotBlocked:  true,
		slotStopped:  true,
	},
	slotBlocked: {
		slotStarting: true,
		slotUp:       true,
		slotStopped:  true,
	},
	slotWaitingForNodes: {
		slotStarting: true,
		slotUp:       true,
		slotBlocked:  true,
		slotStopped:  true,
	},
	slotFailed: {
		slotStarting: true,
		slotStopped:  true,
	},
	slotStopped: {
		slotStarting: true,
		slotStopped:  true,
	},
}

func slotTransitionAllowed(from, to slotPhase) bool {
	if from == "" || from == to {
		return true
	}
	return slotPhaseTransitions[from][to]
}

// setPhaseLocked 在持有 t.mu 时更新状态。转换表之外的边是硬失败：
// 记录 error 并保持原状态，避免后台把 Slot 带进未定义状态。空状态
// 表示未初始化的 Tunnel（测试夹具或构造中的对象），允许进入任意状态。
func (t *Tunnel) setPhaseLocked(to slotPhase) bool {
	from := slotPhase(t.Status)
	if from == to {
		return true
	}
	if !slotTransitionAllowed(from, to) {
		slog.Error("slot.phase.rejected", "slot", t.Slot, "from", from, "to", to)
		return false
	}
	slog.Info("slot.phase", "slot", t.Slot, "from", from, "to", to)
	t.Status = string(to)
	return true
}

// setPhase 是 setPhaseLocked 的加锁版本，供尚未持有 t.mu 的调用方使用。
func (t *Tunnel) setPhase(to slotPhase) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.setPhaseLocked(to)
}
