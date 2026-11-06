package main

import (
	"encoding/json"
	"fanout/internal/paths"
	"os"
	"testing"
	"time"
)

func TestChecksPersistenceDebouncesAndForcesFlush(t *testing.T) {
	c := consoleFixture(t)
	c.m.nodes = []Node{{HostName: "a", Config: "fixture"}}
	c.checksMu.Lock()
	c.results["a"] = CheckResult{Host: "a", Status: "passed", CheckedAt: time.Now().UTC()}
	c.markChecksDirtyLocked()
	c.checksMu.Unlock()
	time.Sleep(100 * time.Millisecond)
	if entries, err := c.checkStore.LoadAll(); err != nil || len(entries) != 0 {
		t.Fatal("result was written immediately instead of debounced", entries, err)
	}
	if err := c.flushChecks(); err != nil {
		t.Fatal(err)
	}
	entries, err := c.checkStore.LoadAll()
	if err != nil || len(entries) != 1 {
		t.Fatal("forced flush did not persist results", entries, err)
	}
	var r CheckResult
	if err := json.Unmarshal(entries["a"].Data, &r); err != nil || r.Status != "passed" {
		t.Fatal("persisted result mismatch", r, err)
	}
	if err := c.flushChecks(); err != nil {
		t.Fatal("clean flush failed")
	}
}

func TestChecksPersistenceCombinesMultipleResultsAndSurvivesRestart(t *testing.T) {
	c := consoleFixture(t)
	c.m.nodes = []Node{{HostName: "a", Config: "fixture"}, {HostName: "b", Config: "fixture"}}
	c.checksMu.Lock()
	c.results["a"] = CheckResult{Host: "a", Status: "passed", CheckedAt: time.Now().UTC()}
	c.markChecksDirtyLocked()
	c.results["b"] = CheckResult{Host: "b", Status: "failed", CheckedAt: time.Now().UTC()}
	c.markChecksDirtyLocked()
	c.checksMu.Unlock()
	if err := c.flushChecks(); err != nil {
		t.Fatal(err)
	}
	reloaded, err := newConsole(NewManager(maximumSlots, c.m.workDir))
	if err != nil {
		t.Fatal(err)
	}
	defer reloaded.cancel()
	reloaded.checksMu.Lock()
	count := len(reloaded.results)
	reloaded.checksMu.Unlock()
	if count != 2 {
		t.Fatalf("reloaded results: %d", count)
	}
}

func TestChecksFlushRemovesLegacyFileInDBMode(t *testing.T) {
	c := consoleFixture(t)
	if _, ok := c.checkStore.(legacyChecksFile); ok {
		t.Skip("SQLite unavailable, fallback mode keeps the file")
	}
	// 模拟升级残留：库里没有、文件里有旧结果。
	legacy := paths.Checks(c.m.workDir)
	if err := os.WriteFile(legacy, []byte(`{"old":{"host":"old","status":"failed"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	c.m.nodes = []Node{{HostName: "b", Config: "fixture"}}
	c.checksMu.Lock()
	c.results["b"] = CheckResult{Host: "b", Status: "passed", CheckedAt: time.Now().UTC()}
	c.markChecksDirtyLocked()
	c.checksMu.Unlock()
	if err := c.flushChecks(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatal("legacy checks file retained in DB mode")
	}
	entries, err := c.checkStore.LoadAll()
	if err != nil || len(entries) != 1 {
		t.Fatalf("store = %v, %v", entries, err)
	}
	if _, ok := entries["b"]; !ok {
		t.Fatalf("flushed result missing: %v", entries)
	}
}

func TestChecksRetentionPrunesExpiredAndRemovedNodes(t *testing.T) {
	c := consoleFixture(t)
	now := time.Now().UTC()
	c.m.nodes = []Node{{HostName: "keep", Config: "fixture"}, {HostName: "old", Config: "fixture"}, {HostName: "stale", Config: "fixture"}, {HostName: "expired", Config: "fixture"}}
	c.ttlMu.Lock()
	c.checkResultTTLMinutes = 120
	c.observationTTLMinutes = 30
	c.ttlMu.Unlock()
	c.checksMu.Lock()
	c.results["keep"] = CheckResult{Host: "keep", Status: "passed", CheckedAt: now}
	c.results["gone"] = CheckResult{Host: "gone", Status: "passed", CheckedAt: now}
	c.results["old"] = CheckResult{Host: "keep", Status: "passed", CheckedAt: now.Add(-9 * time.Hour)}
	c.checksMu.Unlock()

	image := EgressProfile{IP: "203.0.113.9", Protocol: "openvpn", IPType: "unknown", IntelligenceStatus: "passed", QueriedAt: now.Format(time.RFC3339Nano), ExpiresAt: now.Add(time.Hour).Format(time.RFC3339Nano)}
	expired := image
	expired.ExpiresAt = now.Add(-time.Hour).Format(time.RFC3339Nano)
	c.m.mu.Lock()
	c.m.observations["keep"] = CheckResult{Host: "keep", Status: "passed", CheckedAt: now}
	c.m.observations["gone"] = CheckResult{Host: "gone", Status: "passed", CheckedAt: now}
	c.m.observations["stale"] = CheckResult{Host: "keep", Status: "passed", CheckedAt: now.Add(-2 * time.Hour)}
	c.m.localEvidence["keep"] = Node{HostName: "keep", Config: "fixture", OpenVPNStatus: "passed", ExpectedExitIP: image.IP, OpenVPNEgress: &image}
	c.m.localEvidence["gone"] = Node{HostName: "gone", Config: "fixture", OpenVPNStatus: "passed", ExpectedExitIP: image.IP, OpenVPNEgress: &image}
	c.m.localEvidence["expired"] = Node{HostName: "keep", Config: "fixture", OpenVPNStatus: "passed", ExpectedExitIP: expired.IP, OpenVPNEgress: &expired}
	c.m.mu.Unlock()

	c.retainChecks(now)
	c.checksMu.Lock()
	_, keep := c.results["keep"]
	_, gone := c.results["gone"]
	_, old := c.results["old"]
	c.checksMu.Unlock()
	if !keep || gone || old {
		t.Fatalf("results retention wrong: keep=%v gone=%v old=%v", keep, gone, old)
	}
	c.m.mu.RLock()
	_, obsKeep := c.m.observations["keep"]
	_, obsGone := c.m.observations["gone"]
	_, obsStale := c.m.observations["stale"]
	_, localKeep := c.m.localEvidence["keep"]
	_, localGone := c.m.localEvidence["gone"]
	_, localExpired := c.m.localEvidence["expired"]
	c.m.mu.RUnlock()
	if !obsKeep || obsGone || obsStale {
		t.Fatalf("observation retention wrong: keep=%v gone=%v stale=%v", obsKeep, obsGone, obsStale)
	}
	if !localKeep || localGone || localExpired {
		t.Fatalf("local evidence retention wrong: keep=%v gone=%v expired=%v", localKeep, localGone, localExpired)
	}
}

func TestChecksRetentionCapsStoredResults(t *testing.T) {
	c := consoleFixture(t)
	now := time.Now().UTC()
	c.checksMu.Lock()
	for i := 0; i < maxStoredCheckResults+5; i++ {
		host := "host-" + string(rune('a'+i%26)) + "-" + string(rune('a'+i/26))
		c.results[host] = CheckResult{Host: host, Status: "passed", CheckedAt: now.Add(-time.Duration(i) * time.Second)}
	}
	c.checksMu.Unlock()
	c.retainChecks(now)
	c.checksMu.Lock()
	count := len(c.results)
	c.checksMu.Unlock()
	if count > maxStoredCheckResults {
		t.Fatalf("stored results exceeded cap: %d", count)
	}
}
