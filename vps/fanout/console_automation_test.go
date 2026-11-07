package main

import (
	"context"
	"encoding/json"
	"fanout/internal/paths"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestAutomationSettingsMinimumPersistAndRestart(t *testing.T) {
	c := consoleFixture(t)
	w := httptest.NewRecorder()
	c.apiAutomation(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"refresh_minutes":60`) || strings.Contains(w.Body.String(), "check_minutes") {
		t.Fatal("wrong defaults")
	}
	path := paths.Automation(c.m.workDir)
	before, _ := os.ReadFile(path)
	for _, body := range []string{`{}`, `{"refresh_enabled":true,"refresh_minutes":29}`, `{"refresh_enabled":null,"refresh_minutes":60}`, `{"refresh_enabled":true,"refresh_minutes":60,"check_enabled":true}`} {
		w = httptest.NewRecorder()
		c.apiAutomation(w, httptest.NewRequest("POST", "/", strings.NewReader(body)))
		after, _ := os.ReadFile(path)
		if w.Code != 400 || string(after) != string(before) {
			t.Fatal("invalid interval changed settings")
		}
	}
	w = httptest.NewRecorder()
	c.apiAutomation(w, httptest.NewRequest("POST", "/", strings.NewReader(`{"refresh_enabled":false,"refresh_minutes":30}`)))
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	reloaded, err := newConsole(NewManager(maximumSlots, c.m.workDir))
	if err != nil {
		t.Fatal(err)
	}
	defer reloaded.cancel()
	if reloaded.automation.settings != (automationSettings{false, 30}) {
		t.Fatal("restart reset user's schedule")
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("schedule file became public")
	}
}

func TestAutomationRefreshSuccessTriggersOneFullCheck(t *testing.T) {
	c := defaultTargetFixture(t)
	c.m.nodes = []Node{{HostName: "candidate", Config: "fixture"}}
	dir := t.TempDir()
	t.Setenv("FANOUT_VALIDATED_POOL_FILE", filepath.Join(dir, "pool.json"))
	c.probe = func(ctx context.Context, n Node, slot int, progress func(string)) CheckResult {
		return CheckResult{Host: n.HostName, Status: "passed"}
	}
	if err := os.WriteFile(filepath.Join(dir, "pool.json"), []byte(`{"generated_at":"2026-11-07T00:00:00Z","nodes":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	// In-memory nodes stand in for the just-refreshed pool; the marker only
	// records the on-disk generation.
	c.automationTick(time.Now())
	deadline := time.Now().Add(3 * time.Second)
	var j *CheckRun
	for time.Now().Before(deadline) {
		c.checksMu.Lock()
		j = c.check
		c.checksMu.Unlock()
		if j != nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if j == nil {
		t.Fatal("successful refresh did not trigger a full check")
	}
	select {
	case <-j.finished:
	case <-time.After(3 * time.Second):
		t.Fatal("triggered check stuck")
	}
	c.automation.mu.Lock()
	gen := c.automation.check.PoolGen
	c.automation.mu.Unlock()
	if gen == "" {
		t.Fatal("completed check did not record the pool generation")
	}
	before := j
	c.automationTick(time.Now())
	c.checksMu.Lock()
	after := c.check
	c.checksMu.Unlock()
	if after != before {
		t.Fatal("same pool generation was checked twice")
	}
}
func TestAutomationDoesNotOverlapManualCheckAndWaitsAfterCompletion(t *testing.T) {
	c := defaultTargetFixture(t)
	c.m.nodes = []Node{{HostName: "candidate", Config: "fixture"}}
	c.automation.mu.Lock()
	c.automation.settings.RefreshEnabled = false
	c.automation.check.AnchorAt = time.Now().Add(-2 * time.Hour)
	c.automation.mu.Unlock()
	release := make(chan struct{})
	var calls atomic.Int32
	c.probe = func(ctx context.Context, n Node, slot int, progress func(string)) CheckResult {
		calls.Add(1)
		select {
		case <-release:
		case <-ctx.Done():
		}
		return CheckResult{Host: n.HostName, Status: "passed"}
	}
	j, err := c.startCheck(CheckRequest{All: true})
	if err != nil {
		t.Fatal(err)
	}
	c.automationTick(time.Now())
	c.automationTick(time.Now())
	if c.check != j {
		t.Fatal("automatic task replaced manual work")
	}
	close(release)
	select {
	case <-j.finished:
	case <-time.After(3 * time.Second):
		t.Fatal("check stuck")
	}
	c.automationTick(time.Now())
	if calls.Load() != 1 {
		t.Fatal("back-to-back batch ignored completion interval")
	}
	c.automation.mu.Lock()
	state := c.automation.check
	c.automation.mu.Unlock()
	if state.Status != "completed" || state.Passed != 1 || runDue(state, 60, time.Now().Add(59*time.Minute)) {
		t.Fatal("completion time not persisted")
	}
}

func TestAutomationRefreshPrecedesCheckAndFailedSourceKeepsOldCandidates(t *testing.T) {
	c := defaultTargetFixture(t)
	c.m.nodes = []Node{{HostName: "retained", Config: "fixture"}}
	dir := t.TempDir()
	t.Setenv("FANOUT_VALIDATED_POOL_FILE", filepath.Join(dir, "pool.json"))
	script := filepath.Join(dir, "source.py")
	code := "import sys,json,datetime,pathlib\np=pathlib.Path(sys.argv[sys.argv.index('--schedule-dir')+1])\nt=datetime.datetime.now(datetime.timezone.utc).isoformat()\n(p/'refresh-state.json').write_text(json.dumps({'status':'failed','started_at':t,'finished_at':t,'nodes':0}))\nprint(json.dumps({'status':'source_refresh_failed'}))\n"
	if err := os.WriteFile(script, []byte(code), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FANOUT_TOOLBOX_SCRIPT", script)
	c.probe = func(ctx context.Context, n Node, slot int, progress func(string)) CheckResult {
		return CheckResult{Host: n.HostName, Status: "passed"}
	}
	c.automationTick(time.Now())
	c.refreshMu.Lock()
	done := c.refreshDone
	c.refreshMu.Unlock()
	if done == nil {
		t.Fatal("refresh not started")
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("refresh stuck")
	}
	// Failed refresh writes no new pool generation, so no check may start.
	// Completion of the scheduled refresh triggers the next scheduler decision
	// through refreshDone instead of waiting for the next 15-second poll.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		c.automationTick(time.Now())
		c.checksMu.Lock()
		started := c.check != nil
		c.checksMu.Unlock()
		if started {
			t.Fatal("failed refresh triggered a check")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// A successful refresh writes a new pool generation; the next tick must
	// run one full check over the retained candidates.
	if err := os.WriteFile(filepath.Join(dir, "pool.json"), []byte(`{"generated_at":"2026-11-07T00:00:00Z","nodes":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	c.automationTick(time.Now())
	deadline = time.Now().Add(3 * time.Second)
	var j *CheckRun
	for time.Now().Before(deadline) {
		c.checksMu.Lock()
		j = c.check
		c.checksMu.Unlock()
		if j != nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if j == nil {
		t.Fatal("fresh pool was not checked")
	}
	select {
	case <-j.finished:
	case <-time.After(3 * time.Second):
		t.Fatal("triggered check stuck")
	}
	if len(c.m.Tunnels()) != 0 {
		t.Fatal("test unexpectedly created live tunnels")
	}
}
func TestAutomationInterruptedCheckKeepsIntervalAndNoCandidateBackoff(t *testing.T) {
	c := defaultTargetFixture(t)
	started := time.Now().Add(-10 * time.Minute).UTC()
	blob, _ := json.Marshal(automationRun{Status: "running", StartedAt: started})
	if err := os.WriteFile(paths.CheckSchedule(c.m.workDir), blob, 0600); err != nil {
		t.Fatal(err)
	}
	if err := c.loadAutomation(); err != nil {
		t.Fatal(err)
	}
	if c.automation.check.Status != "interrupted" || runDue(c.automation.check, 60, time.Now()) {
		t.Fatal("restart retriggered an interrupted batch too early")
	}
	c.automation.mu.Lock()
	c.automation.settings.RefreshEnabled = false
	c.automation.check.StartedAt = time.Now().Add(-2 * time.Hour)
	c.automation.mu.Unlock()
	c.automationTick(time.Now())
	if c.automation.check.Status != "no_candidates" || runDue(c.automation.check, 60, time.Now()) {
		t.Fatal("empty pool spun automatic checks")
	}
}

func TestAutomationWaitsForLocalRefreshThenChecksNewPool(t *testing.T) {
	c := defaultTargetFixture(t)
	c.m.nodes = []Node{{HostName: "candidate", Config: "fixture"}}
	dir := t.TempDir()
	t.Setenv("FANOUT_VALIDATED_POOL_FILE", filepath.Join(dir, "pool.json"))
	c.probe = func(ctx context.Context, n Node, slot int, progress func(string)) CheckResult {
		return CheckResult{Host: n.HostName, Status: "passed"}
	}
	c.refreshMu.Lock()
	c.refreshState = "running"
	c.refreshMu.Unlock()
	c.automationTick(time.Now())
	c.checksMu.Lock()
	started := c.check != nil
	c.checksMu.Unlock()
	if started {
		t.Fatal("automatic check started during local refresh")
	}
	c.refreshMu.Lock()
	c.refreshState = ""
	c.refreshMu.Unlock()
	// A stale or missing external marker alone starts nothing; only a new
	// on-disk pool generation arms the single post-refresh full check.
	c.automationTick(time.Now())
	c.checksMu.Lock()
	started = c.check != nil
	c.checksMu.Unlock()
	if started {
		t.Fatal("check started without a fresh pool")
	}
	if err := os.WriteFile(filepath.Join(dir, "pool.json"), []byte(`{"generated_at":"2026-11-07T00:00:00Z","nodes":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	c.automationTick(time.Now())
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		c.checksMu.Lock()
		j := c.check
		c.checksMu.Unlock()
		if j != nil {
			select {
			case <-j.finished:
			case <-time.After(3 * time.Second):
				t.Fatal("check stuck")
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("fresh pool generation did not trigger a check")
}
func TestAutomationDoesNotRefreshWhileCheckRunning(t *testing.T) {
	c := defaultTargetFixture(t)
	c.m.nodes = []Node{{HostName: "candidate", Config: "fixture"}}
	c.automation.mu.Lock()
	c.automation.settings.RefreshEnabled = true
	c.automation.check.Status = "running"
	c.automation.check.StartedAt = time.Now().Add(-time.Hour)
	c.automation.mu.Unlock()
	c.automationTick(time.Now())
	c.refreshMu.Lock()
	running, started := c.refreshState == "running", !c.refreshStarted.IsZero()
	c.refreshMu.Unlock()
	if running || started {
		t.Fatal("source refresh started while a check was running")
	}
}
