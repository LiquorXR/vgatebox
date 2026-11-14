package main

// console_sstp_api.go：SSTP HTTP API。
import (
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

func (s *SstpManager) routes(mux *http.ServeMux) {
	mux.HandleFunc("/api/resources/sstp", s.apiView)
	mux.HandleFunc("/api/resources/sstp/settings", s.apiSettings)
	mux.HandleFunc("/api/resources/sstp/check", s.apiStartCheck)
	mux.HandleFunc("/api/resources/sstp/cancel", s.apiCancelCheck)
	mux.HandleFunc("/api/resources/sstp/export", s.apiExport)
}

// apiStartCheck 从当前 SSTP 候选清单启动一次检测，只接受 POST JSON。
func (s *SstpManager) apiStartCheck(w http.ResponseWriter, r *http.Request) {
	var in CheckRequest
	if readConsoleJSON(w, r, &in) != nil {
		writeJSON(w, 400, map[string]string{"error": "请求格式无效"})
		return
	}
	j, err := s.start(in)
	if err != nil {
		writeJSON(w, 409, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 202, j.view())
}

// apiCancelCheck 取消正在运行的 SSTP 检测，只接受 POST。
func (s *SstpManager) apiCancelCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(405)
		return
	}
	s.mu.Lock()
	if s.job != nil {
		s.job.cancel()
	}
	s.mu.Unlock()
	writeJSON(w, 202, map[string]bool{"ok": true})
}

func (s *SstpManager) apiView(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(405)
		return
	}
	nodes, _ := s.c.m.Nodes()
	s.mu.Lock()
	settings, last := s.state.Settings, s.state.LastStarted
	attempts := map[string]CheckResult{}
	for host, result := range s.lastAttempts {
		attempts[host] = result
	}
	var job any
	if s.job != nil {
		s.job.mu.Lock()
		for _, result := range s.job.Items {
			attempts[result.Host] = result
		}
		s.job.mu.Unlock()
		job = s.job.view()
	}
	rows := []map[string]any{}
	for _, n := range nodes {
		if len(n.SSTPCandidates) == 0 {
			continue
		}
		result, ok := s.state.Results[n.HostName]
		if ok {
			result = freshSstpResult(n, result, time.Now())
		} else {
			result = CheckResult{Host: n.HostName, Status: "not_tested", IPType: "unknown", CFStatus: "not_tested", ObservedBy: "vps"}
		}
		row := map[string]any{"hostname": n.HostName, "server_ip": n.IP, "source_country": n.CountryCode, "candidates": n.SSTPCandidates, "result": result}
		if attempt, ok := attempts[n.HostName]; ok && attempt.ProfileSHA == sstpProfile(n) {
			if attempt.Status == "passed" || attempt.Status == "failed" {
				attempt = freshSstpResult(n, attempt, time.Now())
			}
			row["attempt"] = attempt
		}
		rows = append(rows, row)
	}
	s.mu.Unlock()
	dependency := ""
	if s.dependencies != nil {
		if err := s.dependencies(); err != nil {
			dependency = err.Error()
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"nodes": rows, "settings": settings, "last_started": last, "check": job, "dependency_error": dependency, "cf_status": "not_tested"})
}

func (s *SstpManager) apiSettings(w http.ResponseWriter, r *http.Request) {
	var v SstpSettings
	if err := readConsoleJSON(w, r, &v); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	if err := validateSstpSettings(v); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.c.ctx.Err() != nil {
		w.WriteHeader(503)
		return
	}
	before := s.state.Settings
	s.state.Settings = v
	if err := s.persist(); err != nil {
		s.state.Settings = before
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, v)
}

func (s *SstpManager) apiExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(405)
		return
	}
	format := r.URL.Query().Get("format")
	if format != "" && format != "json" && format != "text" {
		http.Error(w, "导出格式无效", 400)
		return
	}
	selected := map[string]bool{}
	if hosts := r.URL.Query().Get("hosts"); hosts != "" {
		for _, host := range strings.Split(hosts, ",") {
			selected[host] = true
		}
	}
	nodes, _ := s.c.m.Nodes()
	rows := []map[string]any{}
	links := []string{}
	now := time.Now().UTC()
	s.mu.Lock()
	for _, n := range nodes {
		if len(selected) > 0 && !selected[n.HostName] {
			continue
		}
		result, ok := s.state.Results[n.HostName]
		if !ok {
			continue
		}
		result = freshSstpResult(n, result, now)
		if result.Status != "passed" || result.Stale {
			continue
		}
		u := &url.URL{Scheme: "sstp", Host: fmt.Sprintf("%s:%d", n.HostName, result.Port), User: url.UserPassword("vpn", "vpn")}
		links = append(links, u.String())
		rows = append(rows, map[string]any{"hostname": n.HostName, "server_ip": n.IP, "port": result.Port, "username": "vpn", "password": "vpn", "source_country": n.CountryCode, "result": result, "uri": u.String()})
	}
	s.mu.Unlock()
	if len(rows) == 0 {
		http.Error(w, "所选范围内没有仍有效的 VPS SSTP 实测结果", 409)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if format == "text" {
		sort.Strings(links)
		w.Header().Set("Content-Disposition", `attachment; filename="vgatebox-sstp.txt"`)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(strings.Join(links, "\n") + "\n"))
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="vgatebox-sstp.json"`)
	writeJSON(w, 200, map[string]any{"schema_version": 1, "kind": "vpngate_vps_sstp_observations", "observed_by": "vps", "generated_at": now, "cf_status": "not_tested", "nodes": rows})
}
