package main

// console_api.go：控制台 HTTP API（console/target/refresh/qr/version）。
import (
	"context"
	"encoding/json"
	"fanout/internal/paths"
	"fmt"
	qrcode "github.com/skip2/go-qrcode"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func readConsoleJSON(w http.ResponseWriter, r *http.Request, v any) error {
	if r.Method != http.MethodPost {
		return fmt.Errorf("只接受 POST")
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<20))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return fmt.Errorf("请求格式无效")
	}
	if d.Decode(new(any)) != io.EOF {
		return fmt.Errorf("只接受一个 JSON 对象")
	}
	return nil
}

func (c *Console) routes(mux *http.ServeMux) {
	c.sstp.routes(mux)
	c.third.routes(mux)
	mux.HandleFunc("/api/console", c.apiConsole)
	mux.HandleFunc("/api/console/version", c.apiConsoleVersion)
	mux.HandleFunc("/api/metrics", c.apiMetrics)
	mux.HandleFunc("/api/console/target", c.apiApplyTarget)
	mux.HandleFunc("/api/console/direct", c.apiHostDirect)
	mux.HandleFunc("/api/console/socks", c.apiPublicSocks)
	mux.HandleFunc("/api/console/refresh", c.apiPoolRefresh)
	mux.HandleFunc("/api/console/source-settings", c.apiSourceSettings)
	mux.HandleFunc("/api/console/automation", c.apiAutomation)
	mux.HandleFunc("/api/console/check/settings", c.apiCheckSettings)
	mux.HandleFunc("/api/console/ippure-cache", c.apiIPPureCache)
	mux.HandleFunc("/api/console/check", c.apiCheck)
	mux.HandleFunc("/api/console/check/cancel", c.apiCheckCancel)
	mux.HandleFunc("/api/console/slot/delete", c.apiDeleteSlot)
	mux.HandleFunc("/api/console/qr", c.apiConsoleQR)
}

// apiApplyTarget 收敛“设置自动出口数量”意图：只接受 POST JSON，
// 仅内置 Xray 模式可用，成功返回 {"ok":true}。
func (c *Console) apiApplyTarget(w http.ResponseWriter, r *http.Request) {
	var p ExitPlan
	if err := readConsoleJSON(w, r, &p); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	if panel, err := openPanel(); err != nil || panel.Kind() != "native" {
		writeJSON(w, 409, map[string]string{"error": "自动数量管理仅用于内置 Xray 模式"})
		return
	}
	if err := c.applyTarget(p); err != nil {
		writeJSON(w, 409, map[string]string{"error": firstLine(err.Error())})
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// apiConsoleQR 把当前订阅链接渲染成二维码 PNG，只接受 GET。
// 参数与 /sub 保持一致（slots/proxies/direct/vpn/third/target），
// 口令仍由 subToken 签发，失败时返回对应 4xx/5xx。
func (c *Console) apiConsoleQR(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(405)
		return
	}
	if _, err := subscriptionSlots(r.URL.Query().Get("slots")); err != nil {
		http.Error(w, "出口选择无效", 400)
		return
	}
	if _, err := subscriptionProxyIDs(r.URL.Query().Get("proxies")); err != nil {
		http.Error(w, "第三方选择无效", 400)
		return
	}
	tok, err := subToken()
	if err != nil {
		http.Error(w, "订阅未就绪", 500)
		return
	}
	link := subFullURL(r, tok)
	if slots := r.URL.Query().Get("slots"); slots != "" {
		link += "&slots=" + slots
	}
	if ids := r.URL.Query().Get("proxies"); ids != "" {
		if r.URL.Query().Get("third") == "0" {
			http.Error(w, "第三方选择无效", 400)
			return
		}
		link += "&proxies=" + ids
	}
	for _, key := range []string{"direct", "vpn", "third"} {
		if v := r.URL.Query().Get(key); v != "" {
			if v != "0" && v != "1" {
				http.Error(w, "订阅范围无效", 400)
				return
			}
			link += "&" + key + "=" + v
		}
	}
	if target := r.URL.Query().Get("target"); target != "" {
		if target != "clash" && target != "mihomo" && target != "base64" && target != "links" {
			http.Error(w, "订阅格式无效", 400)
			return
		}
		link += "&target=" + target
	}
	png, err := qrcode.Encode(link, qrcode.Medium, 256)
	if err != nil {
		http.Error(w, "二维码生成失败", 500)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(png)
}

// apiDeleteSlot removes one explicit automatic exit and reduces the target
// count by one, so the background reconcile does not immediately recreate it.
func (c *Console) apiDeleteSlot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var in struct {
		Slot           int  `json:"slot"`
		CleanupArchive bool `json:"cleanup_archive"`
	}
	if err := readConsoleJSON(w, r, &in); err != nil || in.Slot < 1 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "需要有效的出口编号"})
		return
	}
	if err := c.deleteSlot(in.Slot); err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": firstLine(err.Error())})
		return
	}
	cleanupError := ""
	if in.CleanupArchive {
		if err := c.cleanupArchivedInbounds(in.Slot); err != nil {
			cleanupError = firstLine(err.Error())
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "cleanup_error": cleanupError})
}

// cleanupArchivedInbounds removes the automatic entries left behind by a
// deleted Slot. It only matches managed inbounds for that exact Slot, so
// manual entries and inbounds belonging to other exits are untouched.
func (c *Console) cleanupArchivedInbounds(slot int) error {
	p, err := openPanel()
	if err != nil {
		return err
	}
	list, err := p.Inbounds(liveHosts(c.m))
	if err != nil {
		return err
	}
	ids := make([]int, 0, 1)
	for _, ib := range list {
		if ib.ManagedSlot && ib.BoundSlot == slot {
			ids = append(ids, ib.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	if err := p.DeleteInbounds(ids, c.m.Tunnels()); err != nil {
		return err
	}
	invalidateInbounds()
	return nil
}

// apiConsoleVersion 返回控制台可见状态的单调版本号（小响应，供高频轮询）。
// 前端先轮询它，变化时才拉全量 /api/console；只接受 GET。
func (c *Console) apiConsoleVersion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(405)
		return
	}
	writeJSON(w, 200, map[string]any{"version": c.consoleVersion()})
}

func (c *Console) apiConsole(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(405)
		return
	}
	view := c.m.ExitsOf()
	nodes, fetched := c.m.Nodes()
	ready, stopped := 0, 0
	for _, e := range view.Exits {
		if e.Status == "up" {
			ready++
		}
		if e.Status == "stopped" {
			stopped++
		}
	}
	target := len(view.Exits)
	c.targetMu.Lock()
	plan := c.plan
	planErr := c.planError
	if plan != nil {
		v := *plan
		plan = &v
		target = v.Count
	}
	c.targetMu.Unlock()
	suggestedTarget := defaultExitTarget
	if plan == nil && target > 0 {
		suggestedTarget = target
	}
	c.refreshMu.Lock()
	if c.ctx.Err() != nil {
		c.refreshMu.Unlock()
		w.WriteHeader(503)
		return
	}
	refresh := c.poolRefreshViewLocked()
	c.refreshMu.Unlock()
	c.checksMu.Lock()
	results := make(map[string]CheckResult, len(c.results))
	for k, v := range c.results {
		results[k] = v
	}
	check := c.check
	c.checksMu.Unlock()
	var checkView any
	if check != nil {
		checkView = check.view()
	}
	// Source/profile changes invalidate diagnostic results without calling them
	// currently available. Old checks.json entries without ExpiresAt use
	// CheckedAt plus the currently effective TTL.
	resultTTL := c.checkResultTTLAt(time.Now().UTC())
	now := time.Now().UTC()
	for _, n := range nodes {
		if v, ok := results[n.HostName]; ok {
			expiry := resultExpiry(v, resultTTL)
			if v.ExpiresAt.IsZero() && !expiry.IsZero() {
				v.ExpiresAt = expiry
			}
			if v.CheckedAt.IsZero() || v.ProfileSHA != profileDigest(n.Config) || (!expiry.IsZero() && !now.Before(expiry)) {
				v.Stale = true
			}
			results[n.HostName] = v
		}
	}
	memory := map[string]int64{}
	if blob, err := os.ReadFile("/proc/meminfo"); err == nil {
		for _, line := range strings.Split(string(blob), "\n") {
			var key string
			var kb int64
			if _, err := fmt.Sscanf(line, "%s %d", &key, &kb); err == nil && (key == "MemTotal:" || key == "MemAvailable:") {
				memory[strings.TrimSuffix(key, ":")] = kb * 1024
			}
		}
	}
	generated := ""
	var pool struct {
		GeneratedAt string             `json:"generated_at"`
		Source      poolSourceMetadata `json:"source"`
	}
	if blob, err := os.ReadFile(paths.PoolFile()); err == nil && json.Unmarshal(blob, &pool) == nil {
		generated = pool.GeneratedAt
	}
	serverName := "www.tesla.com"
	if panel, err := openPanel(); err == nil {
		if n, ok := panel.(*Native); ok {
			n.mu.Lock()
			for _, ib := range n.store.Inbounds {
				if ib.Reality != nil && len(ib.Reality.ServerNames) > 0 {
					serverName = ib.Reality.ServerNames[0]
					break
				}
			}
			n.mu.Unlock()
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"version": version, "console_version": c.consoleVersion(), "capacity": c.m.maxSlots, "target": target, "target_configured": plan != nil, "suggested_target": suggestedTarget, "allocated": len(view.Exits), "ready": ready, "stopped": stopped, "waiting": max(0, target-ready-stopped), "candidates": len(nodes), "pool_read_at": fetched, "pool_generated_at": generated, "pool_source": publicPoolSource(pool.Source), "automation": c.automationView(time.Now().UTC()), "default_server_name": serverName, "plan": plan, "plan_error": planErr, "refresh": refresh, "check": checkView, "check_settings": c.checkSettingsView(time.Now().UTC()), "results": results, "regions": c.m.Regions(), "memory": memory})
}

func (c *Console) apiPoolRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(405)
		return
	}
	// A manual refresh does not wake the automatic scheduler: it only changes the
	// pool. The scheduler's own tick picks up whatever is due next, so a manual
	// attempt that fails before recording state cannot retrigger itself.
	if _, err := c.startPoolRefresh(false); err != nil {
		writeJSON(w, 503, map[string]string{"error": "管理服务正在停止"})
		return
	}
	writeJSON(w, 202, map[string]string{"status": "running"})
}

// waitRefreshThenTick lets a completed console refresh wake the scheduler once.
// The scheduler still rechecks every due time and running task before acting.
func (c *Console) waitRefreshThenTick(done <-chan struct{}) {
	if done == nil {
		return
	}
	go func() {
		select {
		case <-done:
			c.automationTick(time.Now().UTC())
		case <-c.ctx.Done():
		}
	}()
}

func (c *Console) startPoolRefresh(scheduled bool) (<-chan struct{}, error) {
	c.refreshMu.Lock()
	if c.ctx.Err() != nil {
		c.refreshMu.Unlock()
		return nil, c.ctx.Err()
	}
	if c.refreshState == "running" {
		done := c.refreshDone
		c.refreshMu.Unlock()
		return done, nil
	}
	c.refreshState, c.refreshError = "running", ""
	c.refreshCause, c.refreshAt = "", ""
	c.refreshStarted = time.Now()
	finished := make(chan struct{})
	c.refreshDone = finished
	c.refreshMu.Unlock()
	c.bumpConsoleVersion()
	log.Print("[pool-refresh] started")
	go func() {
		defer close(finished)
		ctx, cancel := context.WithTimeout(c.ctx, 90*time.Second)
		defer cancel()
		pool := paths.PoolFile()
		args := []string{paths.Toolbox(), "--config", paths.PoolConfig(), "pool", "--output", filepath.Dir(pool), "--schedule-dir", paths.StateDir(c.m.workDir)}
		if scheduled {
			args = append(args, "--scheduled")
		}
		cmd := exec.CommandContext(ctx, "python3", args...)
		var stderr, stdout poolRefreshErrorBuffer
		cmd.Stderr, cmd.Stdout = &stderr, &stdout
		err := cmdRun(cmd)
		cause := poolRefreshCause(ctx, err, string(stderr.data))
		var result struct {
			Status string `json:"status"`
		}
		_ = json.Unmarshal(stdout.data, &result)
		skipped := scheduled && (result.Status == "source_refresh_skipped" || cause == "pool_refresh_busy")
		if err == nil && !skipped {
			_, err = c.m.RefreshNodes()
			if err != nil {
				cause = "pool_refresh_invalid_pool"
			}
		}
		nodes, _ := c.m.Nodes()
		c.refreshMu.Lock()
		defer c.refreshMu.Unlock()
		c.refreshAt = time.Now().UTC().Format(time.RFC3339)
		c.refreshState = "passed"
		if skipped {
			c.refreshState = "skipped"
		} else if err != nil {
			c.refreshState, c.refreshError = "failed", "pool_refresh_failed_previous_pool_retained"
			c.refreshCause = cause
		}
		log.Printf("[pool-refresh] status=%s cause=%s candidates=%d", c.refreshState, c.refreshCause, len(nodes))
		c.bumpConsoleVersion()
	}()
	return finished, nil
}
