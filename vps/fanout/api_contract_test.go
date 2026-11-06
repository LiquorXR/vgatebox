package main

// TestAPIContract 是 P0-1 的 API 契约测试：路由只增不偷改。
//
// 契约三方：
//  1. vps/fanout/routes.go 的 allAPIRoutes()（代码里的表）；
//  2. vps/fanout/testdata/api/routes.json（金样本）；
//  3. main.go / console*.go 里的 mux.HandleFunc 注册点（实际行为）。
//
// 改路由的正确流程：改 allAPIRoutes()，跑
// VGATEBOX_UPDATE_API_GOLDEN=1 go test -run TestAPIContract ./...,
// 再按测试提示同步注册点。金样本与代码表不一致、注册集合与金样本
// 不一致、注册点出现匿名 handler，都会让本测试失败。
//
// 本测试只读文件、只用标准库，不依赖 root/netns/Xray，
// Linux CI 与本地交叉验证均可跑。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
)

type apiGoldenDoc struct {
	SchemaVersion int        `json:"schema_version"`
	Routes        []apiRoute `json:"routes"`
}

var apiRouteFiles = []string{
	"main.go",
	"console.go",
	"console_sstp.go",
	"console_third_party.go",
}

func loadAPIGolden(t *testing.T) apiGoldenDoc {
	t.Helper()
	blob, err := os.ReadFile(filepath.Join("testdata", "api", "routes.json"))
	if err != nil {
		t.Fatalf("读取 API 金样本失败: %v", err)
	}
	var doc apiGoldenDoc
	if err := json.Unmarshal(blob, &doc); err != nil {
		t.Fatalf("API 金样本不是合法 JSON: %v", err)
	}
	return doc
}

func writeAPIGolden(t *testing.T, routes []apiRoute) {
	t.Helper()
	doc := apiGoldenDoc{SchemaVersion: 1, Routes: routes}
	blob, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatalf("序列化 API 金样本失败: %v", err)
	}
	blob = append(blob, '\n')
	if err := os.WriteFile(filepath.Join("testdata", "api", "routes.json"), blob, 0644); err != nil {
		t.Fatalf("写 API 金样本失败: %v", err)
	}
}

func checkAPIRouteEntry(t *testing.T, r apiRoute, seen map[string]bool) {
	t.Helper()
	if r.Path == "" || (!strings.HasPrefix(r.Path, "/") && r.Path != "/") {
		t.Errorf("非法路径 %q", r.Path)
	}
	if seen[r.Path] {
		t.Errorf("重复路径 %q", r.Path)
	}
	seen[r.Path] = true
	if r.Handler == "" {
		t.Errorf("路径 %q 缺少处理函数名", r.Path)
	}
	if len(r.Methods) == 0 {
		t.Errorf("路径 %q 缺少方法", r.Path)
	}
	for _, m := range r.Methods {
		switch m {
		case "GET", "POST", "DELETE", "HEAD":
		default:
			t.Errorf("路径 %q 有未知方法 %q", r.Path, m)
		}
	}
	switch r.Auth {
	case "session", "token", "mixed", "none":
	default:
		t.Errorf("路径 %q 有未知认证方式 %q", r.Path, r.Auth)
	}
}

func TestAPIContract(t *testing.T) {
	table := allAPIRoutes()

	t.Run("GoldenMatchesTable", func(t *testing.T) {
		if os.Getenv("VGATEBOX_UPDATE_API_GOLDEN") == "1" {
			writeAPIGolden(t, table)
			t.Logf("已用 allAPIRoutes() 重写金样本，共 %d 条", len(table))
			return
		}
		doc := loadAPIGolden(t)
		if doc.SchemaVersion != 1 {
			t.Fatalf("schema_version = %d，想要 1", doc.SchemaVersion)
		}
		seen := map[string]bool{}
		for _, r := range doc.Routes {
			checkAPIRouteEntry(t, r, seen)
		}
		if !sort.SliceIsSorted(doc.Routes, func(i, j int) bool { return doc.Routes[i].Path < doc.Routes[j].Path }) {
			t.Error("金样本必须按 path 排序，保证 diff 稳定")
		}
		if !reflect.DeepEqual(doc.Routes, table) {
			t.Error("金样本与 allAPIRoutes() 不一致：改路由请同步改 routes.go，再用 VGATEBOX_UPDATE_API_GOLDEN=1 重写金样本")
		}
	})

	t.Run("HandlesCoverage", func(t *testing.T) {
		doc := loadAPIGolden(t)
		golden := map[string]bool{}
		for _, r := range doc.Routes {
			golden[r.Path] = true
		}
		staticRe := regexp.MustCompile(`mux\.HandleFunc\(\s*"([^"]+)"\s*,`)
		dynamicRe := regexp.MustCompile(`mux\.HandleFunc\(\s*"([^"]*)"\s*\+\s*action`)
		actionsRe := regexp.MustCompile(`for\s+_,\s*action\s*:=\s*range\s*\[\]string\{([^}]*)\}`)
		strRe := regexp.MustCompile(`"([^"]+)"`)
		registered := map[string]string{}
		for _, file := range apiRouteFiles {
			blob, err := os.ReadFile(file)
			if err != nil {
				t.Fatalf("读取 %s 失败: %v", file, err)
			}
			src := string(blob)
			for _, m := range staticRe.FindAllStringSubmatch(src, -1) {
				registered[m[1]] = file
			}
			for _, m := range dynamicRe.FindAllStringSubmatch(src, -1) {
				prefix := m[1]
				am := actionsRe.FindStringSubmatch(src)
				if am == nil {
					t.Fatalf("%s 有动态路由 %q 但找不到 action 列表", file, prefix)
				}
				for _, a := range strRe.FindAllStringSubmatch(am[1], -1) {
					registered[prefix+a[1]] = file + "(action)"
				}
			}
		}
		for path := range golden {
			if _, ok := registered[path]; !ok {
				t.Errorf("金样本 %q 在注册点没有对应的 mux.HandleFunc", path)
			}
		}
		for path, file := range registered {
			if !golden[path] {
				t.Errorf("注册点 %s(%q) 不在金样本里：新路由必须先写入契约", file, path)
			}
		}
		t.Logf("HANDLES 覆盖：金样本 %d 条，注册点 %d 条", len(golden), len(registered))
	})

	t.Run("NoAnonymousRouteHandlers", func(t *testing.T) {
		anonRe := regexp.MustCompile(`mux\.HandleFunc\([^,]+,\s*func\(`)
		for _, file := range apiRouteFiles {
			blob, err := os.ReadFile(file)
			if err != nil {
				t.Fatalf("读取 %s 失败: %v", file, err)
			}
			if loc := anonRe.FindStringIndex(string(blob)); loc != nil {
				line := strings.Count(string(blob)[:loc[0]], "\n") + 1
				t.Errorf("%s:%d 仍有匿名 HandleFunc：请命名后注册，保证图谱 HANDLES 可归因", file, line)
			}
		}
	})

	t.Run("MainDelegatesToRunFanout", func(t *testing.T) {
		blob, err := os.ReadFile("main.go")
		if err != nil {
			t.Fatalf("读取 main.go 失败: %v", err)
		}
		src := string(blob)
		if !strings.Contains(src, "func runFanout(args []string, stdout, stderr io.Writer) int") {
			t.Error("main.go 缺少 func runFanout(args []string, stdout, stderr io.Writer) int")
		}
		if !strings.Contains(src, "os.Exit(runFanout(os.Args[1:], os.Stdout, os.Stderr))") {
			t.Error("main() 必须只剩 os.Exit(runFanout(os.Args[1:], os.Stdout, os.Stderr))")
		}
		if strings.Contains(src, "log.Fatal") {
			t.Error("main.go 不得再用 log.Fatal（它绕过返回码直接退出）：请用 log.Printf + return 1")
		}
	})
}
