package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func scrapeMetrics(t *testing.T, c *Console) string {
	t.Helper()
	rec := httptest.NewRecorder()
	c.apiMetrics(rec, httptest.NewRequest(http.MethodGet, "/api/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/plain") {
		t.Fatalf("Content-Type = %q", ct)
	}
	return rec.Body.String()
}

func TestMetricsShape(t *testing.T) {
	c := consoleFixture(t)
	body := scrapeMetrics(t, c)
	for _, name := range []string{
		"vgatebox_slots_up",
		"vgatebox_slots_blocked",
		"vgatebox_slots_total",
		"vgatebox_pool_age_seconds",
		"vgatebox_xray_restarts_total",
		"vgatebox_check_last_total",
		"vgatebox_check_last_passed",
		"vgatebox_http_requests_total",
	} {
		if !strings.Contains(body, "# HELP "+name) || !strings.Contains(body, "# TYPE "+name) {
			t.Errorf("缺少指标 %s 的 HELP/TYPE 行", name)
		}
	}
	// 空 fixture：无隧道、无自动化之外的状态，计数为 0 而不是缺失。
	for _, line := range []string{"vgatebox_slots_up 0", "vgatebox_slots_total 0"} {
		if !strings.Contains(body, line) {
			t.Errorf("缺少行 %q", line)
		}
	}
}

func TestMetricsMethodGuard(t *testing.T) {
	c := consoleFixture(t)
	rec := httptest.NewRecorder()
	c.apiMetrics(rec, httptest.NewRequest(http.MethodPost, "/api/metrics", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status = %d，想要 405", rec.Code)
	}
}

func TestMetricsCountsHTTPByPathAndStatus(t *testing.T) {
	c := consoleFixture(t)
	stub := withStatusLog(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		stub.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/metrics-test-probe", nil))
	}
	body := scrapeMetrics(t, c)
	want := `vgatebox_http_requests_total{path="/api/metrics-test-probe",status="418"} 2`
	if !strings.Contains(body, want) {
		t.Fatalf("缺少行 %q", want)
	}
}

func TestMetricsPathLabelNeverLeaksRandomBase(t *testing.T) {
	for path, want := range map[string]string{
		"/api/console":        "/api/console",
		"/api/metrics":        "/api/metrics",
		"/sub":                "/sub",
		"/login":              "/login",
		"/healthz":            "/healthz",
		"/next/assets/app.js": "/next/assets/app.js",
		"/assets/app.js":      "/assets/app.js",
		"/aB3xY9pQ/api/sub":   "other",
		"/favicon.svg":        "other",
		"/":                   "/",
	} {
		if got := metricsPathLabel(path); got != want {
			t.Errorf("metricsPathLabel(%q) = %q，想要 %q", path, got, want)
		}
	}
}

func TestMetricsCheckNeverWithoutAutomation(t *testing.T) {
	m := NewManager(maximumSlots, t.TempDir())
	c := &Console{m: m}
	body := scrapeMetrics(t, c)
	for _, line := range []string{"vgatebox_check_last_total -1", "vgatebox_check_last_passed -1"} {
		if !strings.Contains(body, line) {
			t.Errorf("缺少行 %q", line)
		}
	}
}

func TestApplyCountsXrayRestart(t *testing.T) {
	n, _, tunnels := rollbackFixture(t)
	before := xrayRestarts.Load()
	if err := n.apply(tunnels); err != nil {
		t.Fatal(err)
	}
	if got := xrayRestarts.Load() - before; got != 1 {
		t.Fatalf("成功重启后计数增量 = %d，想要 1", got)
	}
}
