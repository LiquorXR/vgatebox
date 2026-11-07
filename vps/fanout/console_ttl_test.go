package main

import (
	"testing"
	"time"
)

func TestFixedCheckResultTTLIsThirtyMinutes(t *testing.T) {
	c := consoleFixture(t)
	defer c.cancel()
	if got := c.checkResultTTLAt(time.Now()); got != 30*time.Minute {
		t.Fatalf("result TTL = %s, want 30m", got)
	}
	if got := resultExpiry(CheckResult{CheckedAt: time.Now().UTC()}, 30*time.Minute); got.IsZero() {
		t.Fatal("expiry was not derived from check time")
	}
}
func TestConfiguredCheckResultTTLAlwaysThirtyMinutes(t *testing.T) {
	for _, configured := range []int{0, 15, 30, 60, 5000} {
		if got := effectiveResultTTL(configured); got != 30*time.Minute {
			t.Fatalf("configured=%d: got %s want 30m", configured, got)
		}
	}
}
func TestObservationTTLKeepsOrderingWithinResultTTL(t *testing.T) {
	if got := effectiveObservationTTL(defaultObservationTTLMinutes, 30*time.Minute); got != 30*time.Minute {
		t.Fatalf("default observation TTL: %s", got)
	}
	if got := effectiveObservationTTL(15, 30*time.Minute); got != 15*time.Minute {
		t.Fatalf("short observation TTL was raised: %s", got)
	}
	if got := effectiveObservationTTL(60, 30*time.Minute); got != 30*time.Minute {
		t.Fatalf("observation TTL exceeded result TTL: %s", got)
	}
	if got := effectiveObservationTTL(2000, 30*time.Minute); got != 30*time.Minute {
		t.Fatalf("oversized observation TTL was kept: %s", got)
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

func TestResultTTLFloorIsThirtyMinutes(t *testing.T) {
	if fixedCheckResultTTLMinutes != 30 {
		t.Fatalf("fixed result TTL = %d, want 30", fixedCheckResultTTLMinutes)
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

func TestOldChecksWithoutExpiryUseFixedTTL(t *testing.T) {
	c := consoleFixture(t)
	defer c.cancel()
	checked := time.Now().UTC().Add(-time.Hour)
	c.checksMu.Lock()
	c.results["old"] = CheckResult{Host: "old", Status: "passed", CheckedAt: checked}
	c.checksMu.Unlock()
	c.migrateCheckResultTTL(time.Now().UTC())
	c.checksMu.Lock()
	result := c.results["old"]
	c.checksMu.Unlock()
	want := checked.Add(30 * time.Minute)
	if !result.ExpiresAt.Equal(want) {
		t.Fatalf("old result expiry: got %s want %s", result.ExpiresAt, want)
	}
	if !resultExpiry(result, 30*time.Minute).Equal(want) {
		t.Fatal("stored expiry was replaced by the current TTL")
	}
}
