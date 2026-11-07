package main

import (
	"testing"
	"time"
)

func TestSwapQueuesWhileStarting(t *testing.T) {
	m := NewManager(4, t.TempDir())
	tun := &Tunnel{Slot: 1, Status: "starting", Node: Node{HostName: "cur"}}
	m.tunnels[1] = tun
	if err := m.Swap(1); err != nil {
		t.Fatalf("starting swap rejected: %v", err)
	}
	if !m.hasPendingSwap(1) || m.hasPendingSwap(2) {
		t.Fatal("pending swap not recorded on the right slot")
	}
}

func TestSwapToQueuesValidatedTargetWhileStarting(t *testing.T) {
	m := NewManager(4, t.TempDir())
	m.nodes = []Node{{HostName: "target", CountryCode: "JP"}}
	tun := &Tunnel{Slot: 1, Status: "starting", Node: Node{HostName: "cur", CountryCode: "JP"}}
	m.tunnels[1] = tun
	if err := m.SwapTo(1, "target"); err != nil {
		t.Fatalf("starting swap-to rejected: %v", err)
	}
	node, auto, ok := tun.takePendingSwap()
	if !ok || auto || node == nil || node.HostName != "target" {
		t.Fatalf("queued target wrong: %+v %v %v", node, auto, ok)
	}
	if tun.hasPendingSwap() {
		t.Fatal("take did not clear the queue")
	}
}

func TestPublishUpConsumesPendingSwap(t *testing.T) {
	m := NewManager(4, t.TempDir())
	tun := &Tunnel{Slot: 1, Status: "starting", Node: Node{HostName: "cur"}, ExitIP: "203.0.113.60"}
	tun.egress = &EgressProfile{IP: "203.0.113.60", Protocol: "openvpn"}
	tun.resetWork()
	m.tunnels[1] = tun
	tun.probePhase("profile")
	tun.queuePendingSwap(nil, true)
	if !m.publishUp(tun, tun.workContext()) {
		t.Fatal("publishUp rejected a valid tunnel")
	}
	if tun.snapshot().Phase != "" {
		t.Fatal("build stage survived publishUp")
	}
	if tun.hasPendingSwap() {
		t.Fatal("pending swap survived publishUp")
	}
	// 空池：排队委托的 Swap 找不到候选即止，不碰已发布的上线状态。
	deadline := time.Now().Add(2 * time.Second)
	for tun.Status != "up" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if tun.Status != "up" {
		t.Fatalf("failed swap disturbed the published tunnel: %s", tun.Status)
	}
}

func TestStopDropsPendingSwap(t *testing.T) {
	m := NewManager(4, t.TempDir())
	tun := &Tunnel{Slot: 1, Status: "starting", Node: Node{HostName: "cur"}}
	m.tunnels[1] = tun
	tun.queuePendingSwap(nil, true)
	if err := m.Stop(1); err != nil {
		t.Fatalf("stop failed: %v", err)
	}
	if tun.hasPendingSwap() {
		t.Fatal("stop kept a stale queued swap")
	}
}

func TestSnapshotPhaseElapsedOnlyWhileStarting(t *testing.T) {
	tun := &Tunnel{Slot: 1, Status: "starting", Node: Node{HostName: "cur"}}
	tun.probeStage = "connecting"
	tun.probeStageAt = time.Now().Add(-65 * time.Second)
	view := tun.snapshot()
	if view.PhaseElapsedS < 60 || view.PhaseElapsedS > 70 {
		t.Fatalf("phase elapsed = %d, want ~65", view.PhaseElapsedS)
	}
	tun.mu.Lock()
	tun.Status = "up"
	tun.mu.Unlock()
	if view := tun.snapshot(); view.PhaseElapsedS != 0 {
		t.Fatalf("elapsed leaked into up snapshot: %d", view.PhaseElapsedS)
	}
}
