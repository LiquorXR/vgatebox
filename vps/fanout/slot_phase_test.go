package main

import "testing"

func TestSlotPhaseServing(t *testing.T) {
	for _, tc := range []struct {
		phase slotPhase
		want  bool
	}{
		{slotUp, true},
		{slotDegraded, true},
		{slotStarting, false},
		{slotBlocked, false},
		{slotWaitingForNodes, false},
		{slotStopped, false},
	} {
		if got := tc.phase.serving(); got != tc.want {
			t.Errorf("%q.serving() = %v; want %v", tc.phase, got, tc.want)
		}
	}
}

func TestSlotTransitionTable(t *testing.T) {
	allowed := [][2]slotPhase{
		{slotStarting, slotUp},
		{slotStarting, slotWaitingForNodes},
		{slotStarting, slotBlocked},
		{slotUp, slotDegraded},
		{slotDegraded, slotUp},
		{slotUp, slotStarting},
		{slotBlocked, slotStarting},
		{slotWaitingForNodes, slotStarting},
		{slotFailed, slotStarting},
		{slotStopped, slotStarting},
		{slotUp, slotStopped},
		{slotBlocked, slotStopped},
		{slotStopped, slotStopped},
	}
	for _, pair := range allowed {
		if !slotTransitionAllowed(pair[0], pair[1]) {
			t.Errorf("transition %q -> %q rejected; want allowed", pair[0], pair[1])
		}
	}

	rejected := [][2]slotPhase{
		{slotStopped, slotUp},
		{slotStopped, slotDegraded},
		{slotStopped, slotBlocked},
		{slotFailed, slotUp},
		{slotWaitingForNodes, slotDegraded},
	}
	for _, pair := range rejected {
		if slotTransitionAllowed(pair[0], pair[1]) {
			t.Errorf("transition %q -> %q allowed; want rejected", pair[0], pair[1])
		}
	}
}

func TestSlotTransitionUnknownPhaseIsPermissive(t *testing.T) {
	// Bare Tunnel fixtures and objects under construction have no status yet.
	// The persistence layer still validates real state on restore.
	if !slotTransitionAllowed("", slotUp) {
		t.Error("empty phase should allow initialization")
	}
}

func TestSlotPhasePersistedStrings(t *testing.T) {
	// state.json is read by older/newer binaries and by Python acceptance
	// tooling; these values are a wire format, not just UI labels.
	want := map[slotPhase]string{
		slotStarting:        "starting",
		slotUp:              "up",
		slotDegraded:        "degraded",
		slotBlocked:         "blocked",
		slotWaitingForNodes: "waiting_for_nodes",
		slotFailed:          "failed",
		slotStopped:         "stopped",
	}
	for phase, text := range want {
		if string(phase) != text {
			t.Errorf("phase %q serializes to %q; want %q", phase, string(phase), text)
		}
	}
}

func TestSetPhaseLockedUpdatesStatus(t *testing.T) {
	tun := &Tunnel{Slot: 7, Status: string(slotUp)}
	tun.setPhaseLocked(slotDegraded)
	if tun.Status != string(slotDegraded) {
		t.Errorf("status = %q; want %q", tun.Status, slotDegraded)
	}
	tun.setPhaseLocked(slotUp)
	if tun.Status != string(slotUp) {
		t.Errorf("status = %q; want %q", tun.Status, slotUp)
	}
}

func TestSetPhaseUsesTunnelLock(t *testing.T) {
	tun := &Tunnel{Slot: 3, Status: string(slotStarting)}
	tun.setPhase(slotUp)
	if tun.Status != string(slotUp) {
		t.Errorf("status = %q; want %q", tun.Status, slotUp)
	}
}
