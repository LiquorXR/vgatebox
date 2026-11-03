package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestChecksPersistenceDebouncesAndForcesFlush(t *testing.T) {
	c := consoleFixture(t)
	c.m.nodes = []Node{{HostName: "a", Config: "fixture"}}
	path := filepath.Join(c.m.workDir, "checks.json")
	c.checksMu.Lock()
	c.results["a"] = CheckResult{Host: "a", Status: "passed", CheckedAt: time.Now().UTC()}
	c.markChecksDirtyLocked()
	c.checksMu.Unlock()
	time.Sleep(100 * time.Millisecond)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("result was written immediately instead of debounced")
	}
	if err := c.flushChecks(); err != nil {
		t.Fatal(err)
	}
	blob, err := os.ReadFile(path)
	if err != nil || len(blob) == 0 {
		t.Fatal("forced flush did not persist results", err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("checks file is not private")
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
	c.m.observations["stale"] = CheckResult{Host: "keep", Status: "passed", CheckedAt: now.Add(-31 * time.Minute)}
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
