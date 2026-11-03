package main

import (
	"testing"
	"time"
)

func TestAutomaticCheckResultTTLMatchesSchedule(t *testing.T) {
	for _, tc := range []struct {
		enabled bool
		minutes int
		want    time.Duration
	}{
		{false, 15, 30 * time.Minute},
		{false, 120, 30 * time.Minute},
		{true, 15, 30 * time.Minute},
		{true, 30, 45 * time.Minute},
		{true, 60, 75 * time.Minute},
		{true, 120, 135 * time.Minute},
		{true, 5, 30 * time.Minute},
		{true, 24 * 60, 24 * time.Hour},
		{true, 525600, 24 * time.Hour},
	} {
		if got := effectiveResultTTL(0, tc.enabled, tc.minutes); got != tc.want {
			t.Fatalf("enabled=%v minutes=%d: got %s want %s", tc.enabled, tc.minutes, got, tc.want)
		}
	}
}

func TestConfiguredCheckResultTTLOverridesAutomatic(t *testing.T) {
	for _, minutes := range []int{15, 30, 60, 120} {
		want := time.Duration(minutes) * time.Minute
		if got := effectiveResultTTL(minutes, true, 15); got != want {
			t.Fatalf("configured %d minutes: got %s", minutes, got)
		}
	}
	if got := effectiveResultTTL(5000, true, 15); got != 24*time.Hour {
		t.Fatalf("result TTL exceeded 24h cap: %s", got)
	}
}

func TestObservationTTLIsThirtyMinutesAndNeverExceedsResultTTL(t *testing.T) {
	if got := effectiveObservationTTL(defaultObservationTTLMinutes, 75*time.Minute); got != 30*time.Minute {
		t.Fatalf("default observation TTL: %s", got)
	}
	if got := effectiveObservationTTL(30, 20*time.Minute); got != 20*time.Minute {
		t.Fatalf("observation TTL exceeded result TTL: %s", got)
	}
	if got := effectiveObservationTTL(60, 45*time.Minute); got != 45*time.Minute {
		t.Fatalf("observation TTL exceeded shorter result TTL: %s", got)
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
	want := checked.Add(75 * time.Minute)
	if !result.ExpiresAt.Equal(want) {
		t.Fatalf("old result expiry: got %s want %s", result.ExpiresAt, want)
	}
	if !resultExpiry(result, 30*time.Minute).Equal(want) {
		t.Fatal("stored expiry was replaced by the current TTL")
	}
}
