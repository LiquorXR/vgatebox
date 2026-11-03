package main

import (
	"testing"
	"time"
)

func TestAutomaticCheckResultTTLMatchesSchedule(t *testing.T) {
	for _, tc := range []struct {
		enabled bool
		minutes int
		cycle   time.Duration
		want    time.Duration
	}{
		{false, 15, 0, 90 * time.Minute},
		{false, 120, 30 * time.Minute, 90 * time.Minute},
		{true, 15, 0, 90 * time.Minute},
		{true, 15, 10 * time.Minute, 90 * time.Minute},
		{true, 30, 20 * time.Minute, 90 * time.Minute},
		{true, 30, 25 * time.Minute, 95 * time.Minute},
		{true, 60, 20 * time.Minute, 115 * time.Minute},
		{true, 120, 30 * time.Minute, 195 * time.Minute},
		{true, 60, 120 * time.Minute, 315 * time.Minute},
		{true, 24 * 60, 0, 24 * time.Hour},
		{true, 525600, 0, 24 * time.Hour},
	} {
		if got := effectiveResultTTL(0, tc.enabled, tc.minutes, tc.cycle); got != tc.want {
			t.Fatalf("enabled=%v minutes=%d cycle=%s: got %s want %s", tc.enabled, tc.minutes, tc.cycle, got, tc.want)
		}
	}
}

func TestConfiguredCheckResultTTLOverridesButKeepsRefreshMargin(t *testing.T) {
	for _, tc := range []struct {
		configured int
		interval   int
		want       time.Duration
	}{
		{30, 15, 30 * time.Minute},
		{60, 30, 60 * time.Minute},
		{30, 60, 75 * time.Minute}, // raised to interval + 15m so results survive the wait
		{5, 15, 30 * time.Minute},  // clamped to the manual minimum
		{5000, 15, 24 * time.Hour},
	} {
		if got := effectiveResultTTL(tc.configured, true, tc.interval, 0); got != tc.want {
			t.Fatalf("configured=%d interval=%d: got %s want %s", tc.configured, tc.interval, got, tc.want)
		}
	}
	if got := effectiveResultTTL(30, false, 60, 0); got != 30*time.Minute {
		t.Fatalf("disabled schedule changed the manual TTL: %s", got)
	}
}

func TestObservationTTLKeepsOrderingThroughTheNextBatch(t *testing.T) {
	if got := effectiveObservationTTL(defaultObservationTTLMinutes, 90*time.Minute, 0, 0); got != 30*time.Minute {
		t.Fatalf("default observation TTL: %s", got)
	}
	if got := effectiveObservationTTL(30, 95*time.Minute, 30*time.Minute, 25*time.Minute); got != 70*time.Minute {
		t.Fatalf("observation TTL did not cover the next batch: %s", got)
	}
	if got := effectiveObservationTTL(30, 60*time.Minute, 30*time.Minute, 25*time.Minute); got != 60*time.Minute {
		t.Fatalf("observation TTL exceeded result TTL: %s", got)
	}
	if got := effectiveObservationTTL(120, 300*time.Minute, 30*time.Minute, 0); got != 120*time.Minute {
		t.Fatalf("configured observation TTL was not honoured: %s", got)
	}
}

func TestCheckCycleEstimateUsesPoolAndParallelism(t *testing.T) {
	if got := checkCycleEstimate(0, 2); got != 0 {
		t.Fatalf("empty pool estimate: %s", got)
	}
	if got := checkCycleEstimate(98, 2); got != 24*time.Minute+30*time.Second {
		t.Fatalf("98 candidates at parallelism 2: %s", got)
	}
	if got := checkCycleEstimate(10, 4); got != 90*time.Second {
		t.Fatalf("10 candidates at parallelism 4: %s", got)
	}
	if got := checkCycleEstimate(10000, 1); got != maxCycleEstimate {
		t.Fatalf("estimate cap: %s", got)
	}
}

func TestCheckCycleDurationPrefersObservedRun(t *testing.T) {
	c := consoleFixture(t)
	c.m.mu.Lock()
	c.m.nodes = make([]Node, 30) // estimate: 15 batches x 30s = 7m30s
	c.m.mu.Unlock()
	start := time.Now().Add(-40 * time.Minute)
	c.automation.mu.Lock()
	c.automation.check.StartedAt = start
	c.automation.check.FinishedAt = start.Add(35 * time.Minute)
	c.automation.mu.Unlock()
	if got := c.checkCycleDuration(); got != 35*time.Minute {
		t.Fatalf("observed batch duration was not used: %s", got)
	}
}

func TestResultTTLValidationKeepsRefreshMargin(t *testing.T) {
	if err := validateResultTTLForSchedule(0, true, 60); err != nil {
		t.Fatalf("automatic TTL rejected: %v", err)
	}
	if err := validateResultTTLForSchedule(70, true, 60); err == nil {
		t.Fatal("TTL shorter than interval + grace was accepted")
	}
	if err := validateResultTTLForSchedule(75, true, 60); err != nil {
		t.Fatalf("TTL at the floor was rejected: %v", err)
	}
	if err := validateResultTTLForSchedule(30, false, 60); err != nil {
		t.Fatalf("disabled schedule rejected a manual TTL: %v", err)
	}
}

func TestRetentionWindowUsesFourResultTTLsWithinOneDay(t *testing.T) {
	if got := retentionWindow(30 * time.Minute); got != 6*time.Hour {
		t.Fatalf("short TTL retention: %s", got)
	}
	if got := retentionWindow(2 * time.Hour); got != 8*time.Hour {
		t.Fatalf("long TTL retention: %s", got)
	}
	if got := retentionWindow(24 * time.Hour); got != 24*time.Hour {
		t.Fatalf("retention cap: %s", got)
	}
}

func TestOldChecksWithoutExpiryUseCurrentTTL(t *testing.T) {
	c := consoleFixture(t)
	c.ttlMu.Lock()
	c.checkResultTTLMinutes = 0
	c.ttlMu.Unlock()
	c.automation.mu.Lock()
	c.automation.settings.CheckEnabled = true
	c.automation.settings.CheckMinutes = 60
	c.automation.mu.Unlock()
	checked := time.Now().UTC().Add(-time.Hour)
	c.checksMu.Lock()
	c.results["old"] = CheckResult{Host: "old", Status: "passed", CheckedAt: checked}
	c.checksMu.Unlock()
	c.migrateCheckResultTTL(time.Now().UTC())
	c.checksMu.Lock()
	result := c.results["old"]
	c.checksMu.Unlock()
	want := checked.Add(90 * time.Minute)
	if !result.ExpiresAt.Equal(want) {
		t.Fatalf("old result expiry: got %s want %s", result.ExpiresAt, want)
	}
	if !resultExpiry(result, 30*time.Minute).Equal(want) {
		t.Fatal("stored expiry was replaced by the current TTL")
	}
}
