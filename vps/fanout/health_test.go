package main

import (
	"context"
	"sync"
	"testing"
	"time"
)

func healthTunnel(slot int, host, exitIP string) *Tunnel {
	return &Tunnel{Slot: slot, Port: 18000 + slot, Status: "up", Node: Node{HostName: host, Config: "fixture-" + host}, ExitIP: exitIP}
}

func TestHealthRoundsWaitForCompletionAndDoNotOverlap(t *testing.T) {
	m := NewManager(8, t.TempDir())
	m.healthEvery = 20 * time.Millisecond
	tunnel := healthTunnel(1, "health", "203.0.113.10")
	m.tunnels[1] = tunnel
	var mu sync.Mutex
	active, maximum := 0, 0
	var starts []time.Time
	m.healthProbe = func(ctx context.Context, _ *Tunnel) (string, float64, error) {
		mu.Lock()
		active++
		if active > maximum {
			maximum = active
		}
		starts = append(starts, time.Now())
		mu.Unlock()
		select {
		case <-ctx.Done():
		case <-time.After(45 * time.Millisecond):
		}
		mu.Lock()
		active--
		mu.Unlock()
		return tunnel.ExitIP, 12, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 140*time.Millisecond)
	defer cancel()
	m.WatchHealth(ctx)
	mu.Lock()
	defer mu.Unlock()
	if len(starts) < 2 {
		t.Fatalf("expected repeated rounds, got %d", len(starts))
	}
	if maximum != 1 {
		t.Fatalf("rounds overlapped: %d", maximum)
	}
	if gap := starts[1].Sub(starts[0]); gap < 60*time.Millisecond {
		t.Fatalf("round interval did not include probe completion: %s", gap)
	}
}

func TestHealthChecksSlowTunnelWithoutBlockingOthers(t *testing.T) {
	m := NewManager(8, t.TempDir())
	m.healthParallelLimit = 8
	slow, fast := healthTunnel(1, "slow", "203.0.113.11"), healthTunnel(2, "fast", "203.0.113.12")
	m.tunnels[1], m.tunnels[2] = slow, fast
	started := make(chan int, 2)
	release := make(chan struct{})
	m.healthProbe = func(ctx context.Context, tunnel *Tunnel) (string, float64, error) {
		started <- tunnel.Slot
		select {
		case <-release:
		case <-ctx.Done():
			return "", 0, ctx.Err()
		}
		return tunnel.ExitIP, 10, nil
	}
	done := make(chan struct{})
	go func() {
		m.healthRound(context.Background(), newHealthRoundState())
		close(done)
	}()
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("a slow tunnel blocked another tunnel's probe")
		}
	}
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("health round stuck")
	}
}

func TestHealthProbeTimeoutAndTwoFailureReconnect(t *testing.T) {
	m := NewManager(8, t.TempDir())
	m.healthProbeWindow = 25 * time.Millisecond
	tunnel := healthTunnel(1, "timeout", "203.0.113.13")
	m.tunnels[1] = tunnel
	var deadlines []time.Duration
	m.healthProbe = func(ctx context.Context, _ *Tunnel) (string, float64, error) {
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Error("health probe had no timeout")
		}
		deadlines = append(deadlines, time.Until(deadline))
		<-ctx.Done()
		return "", 0, ctx.Err()
	}
	reconnects := 0
	m.healthReconnect = func(*Tunnel, string) { reconnects++ }
	fails := newHealthRoundState()
	start := time.Now()
	m.healthRound(context.Background(), fails)
	if elapsed := time.Since(start); elapsed > 250*time.Millisecond {
		t.Fatalf("health timeout was not the configured window: %s", elapsed)
	}
	if reconnects != 0 {
		t.Fatal("one ordinary failure reconnected immediately")
	}
	m.healthRound(context.Background(), fails)
	if reconnects != 1 {
		t.Fatal("two consecutive failures did not reconnect")
	}
	for _, remaining := range deadlines {
		if remaining > 35*time.Millisecond {
			t.Fatal("health probe deadline exceeded configured window")
		}
	}
}

func TestHealthBlocksImmediatelyOnExitChangeAndExpiredPolicy(t *testing.T) {
	m := NewManager(8, t.TempDir())
	tunnel := healthTunnel(1, "changed", "203.0.113.14")
	m.tunnels[1] = tunnel
	probes, reconnects := 0, 0
	m.healthProbe = func(context.Context, *Tunnel) (string, float64, error) {
		probes++
		return "198.51.100.99", 10, nil
	}
	m.healthReconnect = func(*Tunnel, string) { reconnects++ }
	m.healthRound(context.Background(), newHealthRoundState())
	if probes != 1 || reconnects != 1 || tunnel.Status != "blocked" {
		t.Fatalf("exit change was not blocked immediately: probes=%d reconnects=%d status=%s", probes, reconnects, tunnel.Status)
	}

	expired := "2000-01-01T00:00:00Z"
	policy := SlotPolicy{AllowedIPTypes: []string{"unknown"}}
	stale := healthTunnel(2, "stale", "203.0.113.15")
	stale.Policy = &policy
	stale.egress = &EgressProfile{IP: stale.ExitIP, Protocol: "openvpn", IPType: "unknown", IntelligenceStatus: "passed", ExpiresAt: expired}
	stale.egressNodeHost, stale.egressProfileDigest = stale.Node.HostName, profileDigest(stale.Node.Config)
	m.tunnels[2] = stale
	probes, reconnects = 0, 0
	m.healthProbe = func(context.Context, *Tunnel) (string, float64, error) {
		probes++
		return stale.ExitIP, 10, nil
	}
	m.healthRound(context.Background(), newHealthRoundState())
	if probes != 0 || reconnects != 1 || stale.Status != "blocked" {
		t.Fatalf("expired policy was not blocked immediately: probes=%d reconnects=%d status=%s", probes, reconnects, stale.Status)
	}
}

func TestHealthRoundSkipsBusyTunnelWithoutCountingFailure(t *testing.T) {
	m := NewManager(8, t.TempDir())
	m.healthOpWindow = 50 * time.Millisecond
	busy, free := healthTunnel(1, "busy", "203.0.113.30"), healthTunnel(2, "free", "203.0.113.31")
	m.tunnels[1], m.tunnels[2] = busy, free
	probes, reconnects := 0, 0
	m.healthProbe = func(_ context.Context, tunnel *Tunnel) (string, float64, error) {
		probes++
		return tunnel.ExitIP, 10, nil
	}
	m.healthReconnect = func(*Tunnel, string) { reconnects++ }
	busy.opMu.Lock()
	state := newHealthRoundState()
	state.fails[1] = 1 // an existing streak must survive an unobserved round
	start := time.Now()
	m.healthRound(context.Background(), state)
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("busy slot stretched the round: %s", elapsed)
	}
	if state.fails[1] != 1 || state.fails[2] != 0 {
		t.Fatalf("deferred round changed streaks: %v", state.fails)
	}
	if reconnects != 0 || probes != 1 {
		t.Fatalf("deferred round acted on the busy slot: reconnects=%d probes=%d", reconnects, probes)
	}
	busy.opMu.Unlock()
	m.healthRound(context.Background(), state)
	if state.fails[1] != 0 || probes != 2 {
		t.Fatalf("released slot was not probed: probes=%d fails=%v", probes, state.fails)
	}
}

func TestHealthRechecksStatusAfterWaitingForOperation(t *testing.T) {
	m := NewManager(8, t.TempDir())
	m.healthOpWindow = 2 * time.Second
	tunnel := healthTunnel(1, "restart", "203.0.113.33")
	policy := SlotPolicy{AllowedIPTypes: []string{"unknown"}}
	tunnel.Policy = &policy
	probed, reconnects := 0, 0
	m.healthProbe = func(context.Context, *Tunnel) (string, float64, error) {
		probed++
		return tunnel.ExitIP, 10, nil
	}
	m.healthReconnect = func(*Tunnel, string) { reconnects++ }
	tunnel.opMu.Lock()
	done := make(chan healthState, 1)
	go func() {
		state, _ := m.checkTunnelHealth(context.Background(), tunnel)
		done <- state
	}()
	time.Sleep(50 * time.Millisecond)
	tunnel.mu.Lock()
	tunnel.Status = "starting" // a swap started while the probe waited
	tunnel.mu.Unlock()
	tunnel.opMu.Unlock()
	select {
	case state := <-done:
		if state != healthOK {
			t.Fatalf("restarting tunnel judged against stale evidence: state=%v", state)
		}
	case <-time.After(time.Second):
		t.Fatal("health check stuck on the operation lock")
	}
	if probed != 0 || reconnects != 0 {
		t.Fatalf("stale round acted on a restarting tunnel: probes=%d reconnects=%d", probed, reconnects)
	}
	if tunnel.Status == "blocked" {
		t.Fatal("stale evidence blocked a restarting tunnel")
	}
}

func TestProbePairConcurrentUsesIndependentEchoTimeouts(t *testing.T) {
	perEcho := 25 * time.Millisecond
	var mu sync.Mutex
	deadlines := []time.Duration{}
	start := time.Now()
	_, _, err := probePairConcurrent(context.Background(), perEcho, func(ctx context.Context, url string) (exitIPProbe, error) {
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Error("echo probe had no deadline")
		}
		mu.Lock()
		deadlines = append(deadlines, time.Until(deadline))
		mu.Unlock()
		if url == exitIPEchoURLs[0] {
			<-ctx.Done()
			return exitIPProbe{}, ctx.Err()
		}
		return exitIPProbe{ip: "203.0.113.20", latencyMS: 5}, nil
	})
	if err == nil {
		t.Fatal("a slow echo was treated as success")
	}
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Fatalf("echo timeout was not independent: %s", elapsed)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(deadlines) != 2 {
		t.Fatalf("both echoes did not run: %d", len(deadlines))
	}
	for _, remaining := range deadlines {
		if remaining > perEcho+10*time.Millisecond {
			t.Fatal("echo deadline exceeded per-echo timeout")
		}
	}
}

func TestHealthCancellationStopsWaiting(t *testing.T) {
	m := NewManager(8, t.TempDir())
	m.healthEvery = time.Hour
	m.tunnels[1] = healthTunnel(1, "cancel", "203.0.113.16")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { m.WatchHealth(ctx); close(done) }()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("WatchHealth ignored cancellation")
	}
}
