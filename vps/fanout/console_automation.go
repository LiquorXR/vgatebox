package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	defaultRefreshMinutes = 60
	defaultCheckMinutes   = 15
	minRefreshMinutes     = 30
	minCheckMinutes       = 15
	maxAutomationMinutes  = 525600
	// A source refresh marker older than this is treated as interrupted, so a
	// crashed external refresh cannot block automatic checks forever.
	sourceRefreshWindow = 5 * time.Minute
)

type automationSettings struct {
	RefreshEnabled bool `json:"refresh_enabled"`
	RefreshMinutes int  `json:"refresh_minutes"`
	CheckEnabled   bool `json:"check_enabled"`
	CheckMinutes   int  `json:"check_minutes"`
}

type automationRun struct {
	Status     string    `json:"status"`
	AnchorAt   time.Time `json:"anchor_at,omitempty"`
	StartedAt  time.Time `json:"started_at,omitempty"`
	FinishedAt time.Time `json:"finished_at,omitempty"`
	Nodes      int       `json:"nodes"`
	Passed     int       `json:"passed"`
}

type automationManager struct {
	mu       sync.Mutex
	settings automationSettings
	check    automationRun
	err      string
}

func defaultAutomationSettings() automationSettings {
	return automationSettings{RefreshEnabled: true, RefreshMinutes: defaultRefreshMinutes, CheckEnabled: true, CheckMinutes: defaultCheckMinutes}
}
func validateAutomationSettings(s automationSettings) error {
	if s.RefreshMinutes < minRefreshMinutes || s.RefreshMinutes > maxAutomationMinutes {
		return fmt.Errorf("自动刷新间隔须为 30–525600 分钟")
	}
	if s.CheckMinutes < minCheckMinutes || s.CheckMinutes > maxAutomationMinutes {
		return fmt.Errorf("自动检测间隔须为 15–525600 分钟")
	}
	return nil
}

func (c *Console) loadAutomation() error {
	a := &automationManager{settings: defaultAutomationSettings(), check: automationRun{Status: "never", AnchorAt: time.Now().UTC()}}
	for _, item := range []struct {
		name  string
		value any
	}{{"automation.json", &a.settings}, {"check-schedule-state.json", &a.check}} {
		blob, err := os.ReadFile(filepath.Join(c.m.workDir, item.name))
		if os.IsNotExist(err) {
			blob, _ = json.Marshal(item.value)
			if err := atomicConfigWrite(filepath.Join(c.m.workDir, item.name), blob); err != nil {
				return fmt.Errorf("周期设置初始化失败")
			}
		} else {
			var fields map[string]json.RawMessage
			if err != nil || json.Unmarshal(blob, &fields) != nil || fields == nil || json.Unmarshal(blob, item.value) != nil {
				return fmt.Errorf("周期设置或状态损坏，保留文件")
			}
			if item.name == "automation.json" {
				for _, key := range []string{"refresh_enabled", "refresh_minutes", "check_enabled", "check_minutes"} {
					if raw, ok := fields[key]; ok && string(raw) == "null" {
						return fmt.Errorf("周期设置不能为 null")
					}
				}
			}
		}
	}
	if err := validateAutomationSettings(a.settings); err != nil {
		return err
	}
	if a.check.Status == "running" {
		a.check.Status = "interrupted"
	}
	c.automation = a
	return nil
}

func runReference(r automationRun) time.Time {
	if !r.FinishedAt.IsZero() {
		return r.FinishedAt
	}
	if !r.StartedAt.IsZero() {
		return r.StartedAt
	}
	return r.AnchorAt
}

func runDue(r automationRun, minutes int, now time.Time) bool {
	ref := runReference(r)
	return ref.IsZero() || !now.Before(ref.Add(time.Duration(minutes)*time.Minute))
}

func (c *Console) sourceAutomationState() automationRun {
	var r automationRun
	blob, err := os.ReadFile(filepath.Join(c.m.workDir, "refresh-state.json"))
	if err == nil {
		if json.Unmarshal(blob, &r) != nil || runReference(r).IsZero() || (r.Status != "running" && r.Status != "passed" && r.Status != "failed") {
			return automationRun{Status: "invalid", AnchorAt: time.Now().UTC()}
		}
		return r
	}
	if !os.IsNotExist(err) {
		return automationRun{Status: "invalid", AnchorAt: time.Now().UTC()}
	}
	var pool struct {
		GeneratedAt time.Time `json:"generated_at"`
	}
	if blob, err := os.ReadFile(envOr("FANOUT_VALIDATED_POOL_FILE", "/var/lib/vgatebox/pool/node_pool.json")); err == nil && json.Unmarshal(blob, &pool) == nil {
		r.AnchorAt = pool.GeneratedAt
	}
	r.Status = "never"
	return r
}

func runScheduleView(r automationRun, enabled bool, minutes int, now time.Time, source bool) map[string]any {
	running := r.Status == "running"
	if source && running && now.Sub(r.StartedAt) > sourceRefreshWindow {
		running = false
		r.Status = "interrupted"
	}
	next := ""
	if enabled && !running {
		t := runReference(r)
		if t.IsZero() {
			t = now
		} else {
			t = t.Add(time.Duration(minutes) * time.Minute)
		}
		next = t.UTC().Format(time.RFC3339)
	}
	stamp := func(t time.Time) string {
		if t.IsZero() {
			return ""
		}
		return t.UTC().Format(time.RFC3339)
	}
	return map[string]any{"enabled": enabled, "minutes": minutes, "status": r.Status, "running": running, "started_at": stamp(r.StartedAt), "finished_at": stamp(r.FinishedAt), "next_at": next, "nodes": r.Nodes, "passed": r.Passed}
}

func (c *Console) automationView(now time.Time) any {
	if c.automation == nil {
		return nil
	}
	a := c.automation
	a.mu.Lock()
	s, r, e := a.settings, a.check, a.err
	a.mu.Unlock()
	source := c.sourceAutomationState()
	refresh := runScheduleView(source, s.RefreshEnabled, s.RefreshMinutes, now, true)
	check := runScheduleView(r, s.CheckEnabled, s.CheckMinutes, now, false)
	c.refreshMu.Lock()
	localRunning := c.refreshState == "running"
	c.refreshMu.Unlock()
	check["waiting_for_refresh"] = s.CheckEnabled && !check["running"].(bool) && (localRunning || refresh["running"].(bool))
	return map[string]any{"settings": s, "refresh": refresh, "check": check, "error": e}
}

func (c *Console) apiAutomation(w http.ResponseWriter, r *http.Request) {
	if c.automation == nil || c.ctx.Err() != nil {
		w.WriteHeader(503)
		return
	}
	if r.Method == http.MethodPost {
		var in struct {
			RefreshEnabled *bool `json:"refresh_enabled"`
			RefreshMinutes int   `json:"refresh_minutes"`
			CheckEnabled   *bool `json:"check_enabled"`
			CheckMinutes   int   `json:"check_minutes"`
		}
		if readConsoleJSON(w, r, &in) != nil || in.RefreshEnabled == nil || in.CheckEnabled == nil {
			writeJSON(w, 400, map[string]string{"error": "请完整设置两个周期及开关"})
			return
		}
		s := automationSettings{*in.RefreshEnabled, in.RefreshMinutes, *in.CheckEnabled, in.CheckMinutes}
		if err := validateAutomationSettings(s); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		c.automation.mu.Lock()
		blob, _ := json.Marshal(s)
		err := atomicConfigWrite(filepath.Join(c.m.workDir, "automation.json"), blob)
		if err == nil {
			c.automation.settings = s
		}
		c.automation.mu.Unlock()
		if err != nil {
			writeJSON(w, 409, map[string]string{"error": "周期设置保存失败"})
			return
		}
		c.applyTTLSettings(time.Now().UTC())
	} else if r.Method != http.MethodGet {
		w.WriteHeader(405)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, c.automationView(time.Now().UTC()))
}

// Called with checksMu held; automatic work rechecks the saved settings at the
// point of commitment, while manual work still records its completion clock.
func (c *Console) automationCheckStarted(scheduled bool) error {
	if c.automation == nil {
		return nil
	}
	a := c.automation
	a.mu.Lock()
	defer a.mu.Unlock()
	now := time.Now().UTC()
	if scheduled && (!a.settings.CheckEnabled || a.err != "" || a.check.Status == "running" || !runDue(a.check, a.settings.CheckMinutes, now)) {
		return fmt.Errorf("automatic_check_not_due")
	}
	next := automationRun{Status: "running", AnchorAt: a.check.AnchorAt, StartedAt: now}
	blob, _ := json.Marshal(next)
	if err := atomicConfigWrite(filepath.Join(c.m.workDir, "check-schedule-state.json"), blob); err != nil {
		return fmt.Errorf("周期检测状态保存失败")
	}
	a.check = next
	return nil
}

func (c *Console) automationCheckFinished(j *CheckRun) {
	if c.automation == nil {
		return
	}
	c.checksMu.Lock()
	defer c.checksMu.Unlock()
	if c.check != j {
		return
	}
	j.mu.Lock()
	status, total, passed := j.Status, j.Total, 0
	for _, i := range j.Items {
		if i.Status == "passed" {
			passed++
		}
	}
	j.mu.Unlock()
	a := c.automation
	a.mu.Lock()
	defer a.mu.Unlock()
	a.check.Status = "completed"
	if status == "cancelled" {
		a.check.Status = "cancelled"
	}
	a.check.FinishedAt = time.Now().UTC()
	a.check.Nodes = total
	a.check.Passed = passed
	blob, _ := json.Marshal(a.check)
	if atomicConfigWrite(filepath.Join(c.m.workDir, "check-schedule-state.json"), blob) != nil {
		a.err = "周期状态无法保存，自动任务暂停"
	}
}

func (c *Console) automationTick(now time.Time) {
	if c.automation == nil || c.ctx.Err() != nil || currentPanelMode() != "native" {
		return
	}
	a := c.automation
	a.mu.Lock()
	s, check, e := a.settings, a.check, a.err
	a.mu.Unlock()
	if e != "" {
		return
	}
	// A running manual or automatic check owns the diagnostic permits; starting
	// a source refresh now would only compete with it for the same upstreams.
	if check.Status == "running" {
		return
	}
	c.refreshMu.Lock()
	refreshRunning := c.refreshState == "running"
	localFinished := false
	if !refreshRunning {
		if finished, err := time.Parse(time.RFC3339, c.refreshAt); err == nil && now.Sub(finished) < sourceRefreshWindow {
			localFinished = true
		}
	}
	c.refreshMu.Unlock()
	if refreshRunning {
		return
	}
	source := c.sourceAutomationState()
	if source.Status == "invalid" {
		return
	}
	// Wait for an in-flight source refresh. The same bounded window is used by
	// the status view, so a stale running marker cannot block checks forever.
	// A locally completed refresh (including failure) is authoritative and must
	// not be delayed by an external marker that was not updated.
	if source.Status == "running" && !localFinished && now.Sub(source.StartedAt) < sourceRefreshWindow {
		return
	}
	// A refresh that just finished locally (even when it failed before recording
	// state) must not be retried on this same tick, or a broken builder would be
	// spawned in a loop by its own completion callback.
	if s.RefreshEnabled && !localFinished && runDue(source, s.RefreshMinutes, now) {
		done, err := c.startPoolRefresh(true)
		if err == nil && done != nil {
			// A scheduled refresh completes the refresh phase immediately, so
			// detection does not wait until the next 15-second scheduler tick.
			c.waitRefreshThenTick(done)
		}
		return
	}
	if !s.CheckEnabled || !runDue(check, s.CheckMinutes, now) {
		return
	}
	// The external pool watcher may have written a newer snapshot after the
	// last in-memory load. Reload once before choosing candidates; a failure
	// keeps the previous pool and is reported instead of aborting detection.
	if _, err := c.m.RefreshNodes(); err != nil {
		log.Printf("自动检测前重新读取候选池失败，保留当前候选: %v", err)
	}
	nodes, _ := c.m.Nodes()
	if len(nodes) == 0 {
		a.mu.Lock()
		a.check.Status = "no_candidates"
		a.check.FinishedAt = now
		blob, _ := json.Marshal(a.check)
		if atomicConfigWrite(filepath.Join(c.m.workDir, "check-schedule-state.json"), blob) != nil {
			a.err = "周期状态无法保存，自动任务暂停"
		}
		a.mu.Unlock()
		return
	}
	_, _ = c.startCheck(CheckRequest{Kind: "checks", All: true, scheduled: true})
}

func (c *Console) watchAutomation() {
	// Preserve the initial two-minute startup grace; subsequent ticks only
	// inspect due times and do not contact upstream unless a task is due.
	timer := time.NewTimer(2 * time.Minute)
	defer timer.Stop()
	for {
		select {
		case <-c.ctx.Done():
			return
		case now := <-timer.C:
			c.automationTick(now)
			timer.Reset(15 * time.Second)
		}
	}
}
