package main

import (
	"encoding/json"
	"time"
)

// A plain immutable view avoids copying mutexes or reading changing Node/Policy
// fields while a Slot operation is committing a new verified binding.
type tunnelView struct {
	Slot              int            `json:"slot"`
	Port              int            `json:"port"`
	Node              Node           `json:"node"`
	Phase             string         `json:"phase,omitempty"`
	// PhaseElapsedS 是当前阶段已耗时（秒，仅 starting 展示）。
	PhaseElapsedS     int64          `json:"phase_elapsed_s,omitempty"`
	// PendingSwap 表示 starting 期间收到的换节点意向已排队。
	PendingSwap       bool           `json:"pending_swap,omitempty"`
	Status            string         `json:"status"`
	ExitIP            string         `json:"exit_ip"`
	Err               string         `json:"err,omitempty"`
	// LastExitIP/LastExitAt 是上次完整验活通过的出口：重连清空 ExitIP 后
	// 界面仍能展示"上次可用"。只存内存，重启清零。
	LastExitIP string `json:"last_exit_ip,omitempty"`
	LastExitAt string `json:"last_exit_at,omitempty"`
	// RetryAt 是 waiting_for_nodes 的下次重试时刻，界面据此倒计时。
	RetryAt string `json:"retry_at,omitempty"`
	Since             time.Time      `json:"since"`
	Cred              SocksCred      `json:"cred"`
	Policy            *SlotPolicy    `json:"policy,omitempty"`
	LatencyMS         float64        `json:"latency_ms,omitempty"`
	// LatencyTrend 是即时延迟相对平滑值的走向：rising/falling/steady，
	// 空表示暂无足够样本。前端用它画延迟箭头。
	LatencyTrend      string         `json:"latency_trend,omitempty"`
	Egress            *EgressProfile `json:"egress"`
	CanRollbackPolicy bool           `json:"can_rollback_policy"`
}

func (t *Tunnel) snapshot() tunnelView {
	t.mu.Lock()
	defer t.mu.Unlock()
	trend := ""
	if t.LatencyMS > 0 && t.latencyInstantMS > 0 {
		switch {
		case t.latencyInstantMS > t.LatencyMS*1.2:
			trend = "rising"
		case t.latencyInstantMS < t.LatencyMS*0.8:
			trend = "falling"
		default:
			trend = "steady"
		}
	}
	phaseElapsed := int64(0)
	if t.Status == "starting" && !t.probeStageAt.IsZero() {
		if elapsed := time.Since(t.probeStageAt); elapsed > 0 {
			phaseElapsed = int64(elapsed.Seconds())
		}
	}
	pending := t.pendingSwapAuto || t.pendingSwapTo != nil
	view := tunnelView{Slot: t.Slot, Port: t.Port, Node: t.Node, Status: t.Status, Phase: t.probeStage, PhaseElapsedS: phaseElapsed, ExitIP: t.ExitIP, Err: t.Err,
		Since: t.Since, Cred: t.Cred, Policy: t.Policy, LatencyMS: t.LatencyMS, LatencyTrend: trend, PendingSwap: pending, Egress: t.egressSnapshotLocked(false), CanRollbackPolicy: t.previousPolicy != nil}
	if t.lastExitIP != "" {
		view.LastExitIP = t.lastExitIP
		if !t.lastExitAt.IsZero() {
			view.LastExitAt = t.lastExitAt.UTC().Format(time.RFC3339Nano)
		}
	}
	if t.Status == "waiting_for_nodes" && t.nextRetryAt.After(time.Now()) {
		view.RetryAt = t.nextRetryAt.UTC().Format(time.RFC3339Nano)
	}
	if view.Status == "up" && !t.acceptsSocksLocked() {
		view.Status, view.Err = "blocked", "当前出口证据不满足策略，等待重新验活"
	}
	return view
}

func (t *Tunnel) MarshalJSON() ([]byte, error) { return json.Marshal(t.snapshot()) }
