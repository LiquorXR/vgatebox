package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func nodesPagingFixture(t *testing.T, n int) *Manager {
	t.Helper()
	m := NewManager(maximumSlots, t.TempDir())
	for i := 0; i < n; i++ {
		m.nodes = append(m.nodes, Node{HostName: fmt.Sprintf("node-%03d", i)})
	}
	return m
}

func getNodes(t *testing.T, m *Manager, query string) (int, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	apiNodes(m)(rec, httptest.NewRequest(http.MethodGet, "/api/nodes"+query, nil))
	var view map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("query %q 返回非法 JSON: %v", query, err)
	}
	return rec.Code, view
}

func nodeHostnames(t *testing.T, view map[string]any) []string {
	t.Helper()
	raw, ok := view["nodes"].([]any)
	if !ok {
		t.Fatalf("nodes 形状不对: %T", view["nodes"])
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		node, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("node 形状不对: %T", item)
		}
		host, _ := node["HostName"].(string)
		if host == "" {
			host, _ = node["hostname"].(string)
		}
		out = append(out, host)
	}
	return out
}

// 不带 limit 时字段与旧行为完全一致，不多不少。
func TestNodesNoLimitKeepsLegacyShape(t *testing.T) {
	m := nodesPagingFixture(t, 5)
	code, view := getNodes(t, m, "?all=1")
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	for _, key := range []string{"nodes", "fetched", "total", "available", "residential_only"} {
		if _, ok := view[key]; !ok {
			t.Fatalf("缺少旧字段 %q", key)
		}
	}
	for _, key := range []string{"limit", "offset", "next_offset"} {
		if _, ok := view[key]; ok {
			t.Fatalf("旧行为不应出现字段 %q", key)
		}
	}
	if view["total"] != float64(5) || view["available"] != float64(5) {
		t.Fatalf("total/available = %v/%v", view["total"], view["available"])
	}
	if got := nodeHostnames(t, view); len(got) != 5 || got[0] != "node-000" || got[4] != "node-004" {
		t.Fatalf("nodes = %v", got)
	}
}

func TestNodesPagination(t *testing.T) {
	m := nodesPagingFixture(t, 5)
	code, view := getNodes(t, m, "?all=1&limit=2&offset=1")
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if got := nodeHostnames(t, view); len(got) != 2 || got[0] != "node-001" || got[1] != "node-002" {
		t.Fatalf("page = %v", got)
	}
	if view["total"] != float64(5) || view["available"] != float64(5) {
		t.Fatalf("total/available = %v/%v", view["total"], view["available"])
	}
	if view["limit"] != float64(2) || view["offset"] != float64(1) || view["next_offset"] != float64(3) {
		t.Fatalf("limit/offset/next_offset = %v/%v/%v", view["limit"], view["offset"], view["next_offset"])
	}
}

func TestNodesPaginationEndsWithNullNext(t *testing.T) {
	m := nodesPagingFixture(t, 5)
	_, view := getNodes(t, m, "?all=1&limit=2&offset=4")
	if got := nodeHostnames(t, view); len(got) != 1 || got[0] != "node-004" {
		t.Fatalf("tail page = %v", got)
	}
	if view["next_offset"] != nil {
		t.Fatalf("尾页 next_offset = %v，想要 null", view["next_offset"])
	}
	_, view = getNodes(t, m, "?all=1&limit=2&offset=9")
	if got := nodeHostnames(t, view); len(got) != 0 {
		t.Fatalf("越界 offset nodes = %v", got)
	}
	if view["next_offset"] != nil {
		t.Fatalf("越界 offset next_offset = %v，想要 null", view["next_offset"])
	}
}

func TestNodesPaginationRejectsBadParams(t *testing.T) {
	m := nodesPagingFixture(t, 5)
	for _, query := range []string{
		"?all=1&limit=0", "?all=1&limit=-2", "?all=1&limit=abc",
		"?all=1&limit=1001", "?all=1&limit=2&offset=-1", "?all=1&limit=2&offset=abc",
	} {
		code, _ := getNodes(t, m, query)
		if code != http.StatusBadRequest {
			t.Fatalf("query %q status = %d，想要 400", query, code)
		}
	}
}
