package main

// TestPayloadContract 是 P2-3 的载荷契约：后端应答与前端夹具共享同一份
// required 键表（vps/fanout/testdata/api/payloads.json），只增键不断键。
//
// 本文件是后端侧：直接调 handler（绕过 Auth，认证由路由契约覆盖），
// 断言每个端点的 required 键全在应答顶层。只用标准库，不依赖 root/netns。
// 前端侧：src/api/__tests__/payload-contract.spec.ts 断言同一份表
// 全在 test/fixtures.ts 的 apiPayloads 里。两边都红，说明有人改了字段名
// 却只改了一边。

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

type payloadEndpoint struct {
	Path     string   `json:"path"`
	Query    string   `json:"query,omitempty"`
	Fixture  *bool    `json:"fixture,omitempty"`
	Required []string `json:"required"`
	Numbers  []string `json:"numbers,omitempty"`
	Note     string   `json:"note,omitempty"`
}

type payloadContractDoc struct {
	SchemaVersion int               `json:"schema_version"`
	Endpoints     []payloadEndpoint `json:"endpoints"`
}

func loadPayloadContract(t *testing.T) payloadContractDoc {
	t.Helper()
	blob, err := os.ReadFile(filepath.Join("testdata", "api", "payloads.json"))
	if err != nil {
		t.Fatalf("读取载荷契约失败: %v", err)
	}
	var doc payloadContractDoc
	if err := json.Unmarshal(blob, &doc); err != nil {
		t.Fatalf("载荷契约不是合法 JSON: %v", err)
	}
	if doc.SchemaVersion != 1 {
		t.Fatalf("schema_version = %d，想要 1", doc.SchemaVersion)
	}
	seen := map[string]bool{}
	prevPath, prevQuery := "", ""
	for _, e := range doc.Endpoints {
		key := e.Path + "?" + e.Query
		if e.Path == "" || e.Path[0] != '/' {
			t.Errorf("非法路径 %q", e.Path)
		}
		if seen[key] {
			t.Errorf("重复条目 %q", key)
		}
		seen[key] = true
		if e.Path < prevPath || (e.Path == prevPath && e.Query < prevQuery) {
			t.Errorf("条目 %q 未按 (path, query) 排序，保证 diff 稳定", key)
		}
		prevPath, prevQuery = e.Path, e.Query
		if len(e.Required) == 0 {
			t.Errorf("条目 %q 没有 required 键", key)
		}
		if !sort.StringsAreSorted(e.Required) {
			t.Errorf("条目 %q 的 required 未排序，保证 diff 稳定", key)
		}
	}
	return doc
}

func invokePayloadEndpoint(t *testing.T, path, query string) map[string]any {
	t.Helper()
	target := path
	if query != "" {
		target += "?" + query
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	switch path {
	case "/api/console":
		c := consoleFixture(t)
		c.apiConsole(rec, req)
	case "/api/console/version":
		c := consoleFixture(t)
		c.apiConsoleVersion(rec, req)
	case "/api/exits":
		// hostPublicIP 探活在无网环境最多拖 15s：测试内固定覆盖值。
		setPublicIPOverride("203.0.113.2")
		t.Cleanup(func() { setPublicIPOverride("") })
		apiExits(NewManager(maximumSlots, t.TempDir()))(rec, req)
	case "/api/nodes":
		apiNodes(NewManager(maximumSlots, t.TempDir()))(rec, req)
	case "/api/settings":
		auth, srv, _ := managementFixture(t)
		apiSettings(auth, srv)(rec, req)
	case "/api/sub":
		// SubToken 为空时 handler 会生成并落盘：managementFixture 已备好目录与全局。
		_, _, _ = managementFixture(t)
		apiSub(rec, req)
	case "/healthz":
		handleHealthz(NewManager(maximumSlots, t.TempDir()))(rec, req)
	default:
		t.Fatalf("契约含未知路径 %q：先实现 handler 调用再加表", path)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("%s status = %d", target, rec.Code)
	}
	var v map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil || v == nil {
		t.Fatalf("%s 返回非法 JSON 对象: %v", target, err)
	}
	return v
}

func TestPayloadContract(t *testing.T) {
	doc := loadPayloadContract(t)
	for _, e := range doc.Endpoints {
		e := e
		name := e.Path
		if e.Query != "" {
			name += "?" + e.Query
		}
		t.Run(name, func(t *testing.T) {
			got := invokePayloadEndpoint(t, e.Path, e.Query)
			for _, k := range e.Required {
				if _, ok := got[k]; !ok {
					t.Errorf("%s 应答缺键 %q", name, k)
				}
			}
			for _, k := range e.Numbers {
				if _, ok := got[k].(float64); !ok {
					t.Errorf("%s 键 %q 应为 JSON 数字，实际 %T", name, k, got[k])
				}
			}
		})
	}
}
