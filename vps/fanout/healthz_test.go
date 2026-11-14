package main

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func decodeHealthz(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatalf("healthz 返回非法 JSON: %v", err)
	}
	return v
}

func TestHealthzShape(t *testing.T) {
	m := NewManager(maximumSlots, t.TempDir())
	rec := httptest.NewRecorder()
	handleHealthz(m)(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	v := decodeHealthz(t, rec.Body.Bytes())
	if v["status"] != "ok" {
		t.Fatalf("status = %v", v["status"])
	}
	if _, ok := v["version"].(string); !ok {
		t.Fatal("缺少 version")
	}
	slots, ok := v["slots"].(map[string]any)
	if !ok || slots["total"] == nil || slots["up"] == nil {
		t.Fatalf("slots 形状不对: %v", v["slots"])
	}
	pool, ok := v["pool"].(map[string]any)
	if !ok || pool["candidates"] == nil {
		t.Fatalf("pool 形状不对: %v", v["pool"])
	}
	panel, ok := v["panel"].(map[string]any)
	if !ok || panel["available"] == nil || panel["kind"] == nil {
		t.Fatalf("panel 形状不对: %v", v["panel"])
	}
}

func TestHealthzMethodGuard(t *testing.T) {
	m := NewManager(maximumSlots, t.TempDir())
	rec := httptest.NewRecorder()
	handleHealthz(m)(rec, httptest.NewRequest(http.MethodPost, "/healthz", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status = %d，想要 405", rec.Code)
	}
}

func TestHealthzRedactsQuery(t *testing.T) {
	m := NewManager(maximumSlots, t.TempDir())
	const secret = "SECRET-TOKEN-12345"
	rec := httptest.NewRecorder()
	handleHealthz(m)(rec, httptest.NewRequest(http.MethodGet, "/healthz?token="+secret, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), secret) {
		t.Fatal("healthz 响应体泄漏了 query 参数")
	}
}

func TestRequestIDHeader(t *testing.T) {
	next := withRequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requestIDOf(r.Context()) == "" {
			t.Error("context 里没有 request id")
		}
		w.WriteHeader(http.StatusOK)
	}))
	seen := map[string]bool{}
	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		next.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
		id := rec.Header().Get(requestIDHeader)
		if len(id) != 16 {
			t.Fatalf("request id = %q，想要 16 位 hex", id)
		}
		seen[id] = true
	}
	if len(seen) != 2 {
		t.Fatal("request id 没有每次请求唯一")
	}
}

func TestRequestLogRedactsQuery(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	defer slog.SetDefault(old)

	next := withRequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	rec := httptest.NewRecorder()
	next.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz?token=SECRET-TOKEN-12345", nil))
	line := buf.String()
	if !strings.Contains(line, `"path":"/healthz"`) {
		t.Fatalf("访问日志缺 path：%s", line)
	}
	if strings.Contains(line, "SECRET-TOKEN-12345") {
		t.Fatalf("访问日志泄漏了 query：%s", line)
	}
	if !strings.Contains(line, `"request_id"`) {
		t.Fatalf("访问日志缺 request_id：%s", line)
	}
}

func TestHealthzBypassesLogin(t *testing.T) {
	auth := &Auth{sessions: map[string]time.Time{}, fails: map[string]*loginFails{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", handleHealthz(NewManager(maximumSlots, t.TempDir())))
	mux.HandleFunc("/api/exits", apiExits(NewManager(maximumSlots, t.TempDir())))
	rec := httptest.NewRecorder()
	auth.Wrap(mux).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("未登录 /healthz status = %d，想要 200", rec.Code)
	}
	rec = httptest.NewRecorder()
	auth.Wrap(mux).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/exits", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("未登录 /api/exits status = %d，想要 401（对照组）", rec.Code)
	}
}
