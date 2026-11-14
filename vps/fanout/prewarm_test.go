package main

import (
	"context"
	"testing"
	"time"
)

func TestNoteCandidateFailedBackoffCaps(t *testing.T) {
	tun := &Tunnel{Slot: 1}
	tun.noteCandidateFailed("h")
	if d := time.Until(tun.cooldown["h"]); d <= 25*time.Second || d > 31*time.Second {
		t.Fatalf("first backoff = %s, want ~30s", d)
	}
	tun.noteCandidateFailed("h")
	if d := time.Until(tun.cooldown["h"]); d <= 55*time.Second || d > 61*time.Second {
		t.Fatalf("second backoff = %s, want ~60s", d)
	}
	for i := 0; i < 5; i++ {
		tun.noteCandidateFailed("h")
	}
	if d := time.Until(tun.cooldown["h"]); d <= 115*time.Second || d > 121*time.Second {
		t.Fatalf("capped backoff = %s, want ~120s", d)
	}
	tun.clearFlap()
	if len(tun.failCounts) != 0 {
		t.Fatal("manual clear did not reset backoff counts")
	}
}

func TestCandidatesForSkipsRejectedCurrent(t *testing.T) {
	m := NewManager(4, t.TempDir())
	m.nodes = []Node{
		{HostName: "slow", Config: "s", SpeedMbps: 900},
		{HostName: "fresh", Config: "f", SpeedMbps: 100},
	}
	tun := &Tunnel{Slot: 1, Status: "up", Node: m.nodes[0]}
	m.tunnels[1] = tun
	tun.rememberSwap("slow") // 性能升级时记入的慢节点
	got := m.candidatesFor(tun)
	if len(got) == 0 || got[0].HostName != "fresh" {
		t.Fatalf("rejected current still headed the round: %v", got)
	}
	for _, n := range got {
		if n.HostName == "slow" {
			t.Fatal("rejected node came back as alternative")
		}
	}
}

func TestPrewarmPreferredAndConsumed(t *testing.T) {
	m := NewManager(4, t.TempDir())
	m.nodes = []Node{
		{HostName: "slow", Config: "s", SpeedMbps: 900},
		{HostName: "fast", Config: "f", SpeedMbps: 100},
	}
	tun := &Tunnel{Slot: 1, Status: "degraded", Node: m.nodes[0]}
	m.tunnels[1] = tun
	tun.setPrewarm(m.nodes[1])
	got := m.candidatesFor(tun)
	// 打头仍是当前（恢复语义），热备紧随其后先于常规排序。
	if len(got) < 2 || got[0].HostName != "slow" || got[1].HostName != "fast" {
		t.Fatalf("prewarm not preferred: %v", got)
	}
}

func TestStandbyKeptForUpTunnel(t *testing.T) {
	m := NewManager(4, t.TempDir())
	m.nodes = []Node{
		{HostName: "cur", Config: "c", SpeedMbps: 900},
		{HostName: "standby", Config: "s", SpeedMbps: 100},
	}
	tun := &Tunnel{Slot: 1, Status: "up", Node: m.nodes[0]}
	m.tunnels[1] = tun
	tun.setPrewarm(m.nodes[1])
	got := m.candidatesFor(tun)
	if len(got) < 2 || got[1].HostName != "standby" {
		t.Fatalf("up-tunnel standby not preferred: %v", got)
	}
}

func TestPrewarmDroppedWhenPoolChanged(t *testing.T) {
	m := NewManager(4, t.TempDir())
	m.nodes = []Node{{HostName: "other", Config: "o"}}
	tun := &Tunnel{Slot: 1, Status: "degraded", Node: Node{HostName: "slow", Config: "s"}}
	m.tunnels[1] = tun
	tun.setPrewarm(Node{HostName: "fast", Config: "f"})
	got := m.candidatesFor(tun)
	for _, n := range got {
		if n.HostName == "fast" {
			t.Fatal("stale-config prewarm was offered")
		}
	}
	if pw := tun.prewarmFresh(); pw == nil {
		t.Fatal("fresh prewarm misread as expired")
	}
	tun.prewarmAt = time.Now().Add(-6 * time.Minute)
	if pw := tun.prewarmFresh(); pw != nil {
		t.Fatal("expired prewarm still fresh")
	}
}

func TestRunPrewarmStoresAndDrops(t *testing.T) {
	m := NewManager(4, t.TempDir())
	node := Node{HostName: "cand", Config: "c"}
	m.nodes = []Node{node}
	m.console = &Console{m: m, ctx: context.Background()}
	m.console.probe = func(context.Context, Node, int, func(string)) CheckResult {
		return CheckResult{Host: "cand", Status: "passed"}
	}
	tun := &Tunnel{Slot: 1, Status: "degraded", Node: Node{HostName: "slow", Config: "s"}}
	m.tunnels[1] = tun
	m.runPrewarm(tun, node)
	if pw := tun.prewarmFresh(); pw == nil || pw.HostName != "cand" {
		t.Fatalf("passed prewarm not stored: %v", pw)
	}
	// up 态同样保留为常驻备胎：猝死时直接消费，不必等 degraded 才现验。
	tun2 := &Tunnel{Slot: 2, Status: "up", Node: Node{HostName: "slow2", Config: "s"}}
	m.tunnels[2] = tun2
	m.runPrewarm(tun2, node)
	if pw := tun2.prewarmFresh(); pw == nil || pw.HostName != "cand" {
		t.Fatalf("up-tunnel standby not stored: %v", pw)
	}
	// 无 Console（单测裸 Manager）：静默跳过。
	m.console = nil
	tun3 := &Tunnel{Slot: 3, Status: "degraded", Node: Node{HostName: "slow3", Config: "s"}}
	m.tunnels[3] = tun3
	m.maybePrewarm(tun3)
	if tun3.prewarming {
		t.Fatal("prewarm started without a console")
	}
}

func TestMaintainStandbyThrottles(t *testing.T) {
	m := NewManager(4, t.TempDir())
	m.nodes = []Node{{HostName: "cand", Config: "c"}}
	m.console = &Console{m: m, ctx: context.Background()}
	release := make(chan struct{})
	m.console.probe = func(ctx context.Context, _ Node, _ int, _ func(string)) CheckResult {
		select {
		case <-release:
			return CheckResult{Host: "cand", Status: "passed"}
		case <-ctx.Done():
			return CheckResult{Host: "cand", Status: "cancelled"}
		}
	}
	newUp := func() *Tunnel {
		tun := &Tunnel{Slot: 1, Status: "up", Node: Node{HostName: "cur", Config: "cur"}}
		m.tunnels[1] = tun
		return tun
	}
	// 已有新鲜备胎：不动。
	tun := newUp()
	tun.setPrewarm(Node{HostName: "cand", Config: "c"})
	m.maybeMaintainStandby(tun)
	if tun.prewarming {
		t.Fatal("standby refreshed despite a fresh prewarm")
	}
	// 正在飞：不动。
	tun = newUp()
	tun.prewarming = true
	m.maybeMaintainStandby(tun)
	// 上次尝试太近：不动（且不抢占名额）。
	tun = newUp()
	tun.prewarmRetryAt = time.Now()
	m.maybeMaintainStandby(tun)
	if tun.prewarming {
		t.Fatal("standby retried within the throttle window")
	}
	// 无候选：尝试即放回，不占名额。
	tun = newUp()
	m.nodes = nil
	tun.prewarmRetryAt = time.Now().Add(-2 * standbyRefreshInterval)
	m.maybeMaintainStandby(tun)
	if tun.prewarming {
		t.Fatal("standby held the flight slot with no candidates")
	}
	// 节流过期且有候选：发起一路（阻塞式 probe 保持在飞可观测）。
	m.nodes = []Node{{HostName: "cand", Config: "c"}}
	tun = newUp()
	tun.prewarmRetryAt = time.Now().Add(-2 * standbyRefreshInterval)
	m.maybeMaintainStandby(tun)
	if !tun.prewarming {
		t.Fatal("standby did not start after the throttle window")
	}
	close(release)
	deadline := time.Now().Add(5 * time.Second)
	for tun.prewarmFresh() == nil && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if pw := tun.prewarmFresh(); pw == nil || pw.HostName != "cand" {
		t.Fatalf("standby probe did not land: %v", pw)
	}
}

func TestPerfFloorRotatePredicate(t *testing.T) {
	if !shouldRotateForPerf(100, 200) {
		t.Fatal("clearly worse exit kept")
	}
	if shouldRotateForPerf(100, 150) || shouldRotateForPerf(100, 149) {
		t.Fatal("borderline exit rotated")
	}
	if shouldRotateForPerf(0, 9999) {
		t.Fatal("zero floor rotated")
	}
}

func TestClearSpeculation(t *testing.T) {
	tun := &Tunnel{Slot: 1}
	tun.setPrewarm(Node{HostName: "x"})
	tun.setPerfFloor(100)
	tun.clearSpeculation()
	if tun.prewarmFresh() != nil {
		t.Fatal("prewarm survived manual clear")
	}
	if _, ok := tun.takePerfFloor(); ok {
		t.Fatal("perf floor survived manual clear")
	}
}
