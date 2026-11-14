package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// 版本号是 /api/console 全量拉取的唯一依据：只增不减，
// 每次 apiConsole 可见变更恰好递增至少一次（多增无害，少增则漏更新）。
func TestConsoleVersionStartsAtZero(t *testing.T) {
	c := consoleFixture(t)
	if got := c.consoleVersion(); got != 0 {
		t.Fatalf("初始版本号 = %d，想要 0", got)
	}
}

func TestConsoleVersionBumpsOnSavePlan(t *testing.T) {
	c := consoleFixture(t)
	p := ExitPlan{Count: 1, Policy: defaultSlotPolicyForMode("", false), ServerName: "fixture.example.invalid"}
	if err := c.savePlan(p); err != nil {
		t.Fatal(err)
	}
	if got := c.consoleVersion(); got == 0 {
		t.Fatal("savePlan 成功后版本号未递增")
	}
}

func TestConsoleVersionBumpsOnSaveState(t *testing.T) {
	c := consoleFixture(t)
	before := c.consoleVersion()
	if err := c.m.saveState(); err != nil {
		t.Fatal(err)
	}
	if c.consoleVersion() <= before {
		t.Fatal("saveState 成功后版本号未递增")
	}
}

func TestConsoleVersionBumpsOnChecksDirty(t *testing.T) {
	c := consoleFixture(t)
	before := c.consoleVersion()
	c.checksMu.Lock()
	c.markChecksDirtyLocked()
	c.checksMu.Unlock()
	if c.consoleVersion() <= before {
		t.Fatal("markChecksDirtyLocked 后版本号未递增")
	}
}

func TestConsoleVersionEndpoint(t *testing.T) {
	c := consoleFixture(t)
	c.bumpConsoleVersion()
	c.bumpConsoleVersion()
	rec := httptest.NewRecorder()
	c.apiConsoleVersion(rec, httptest.NewRequest(http.MethodGet, "/api/console/version", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var v struct {
		Version uint64 `json:"version"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	if v.Version != 2 {
		t.Fatalf("version = %d，想要 2", v.Version)
	}
	rec2 := httptest.NewRecorder()
	c.apiConsoleVersion(rec2, httptest.NewRequest(http.MethodPost, "/api/console/version", nil))
	if rec2.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status = %d，想要 405", rec2.Code)
	}
}

func TestRefreshNodesFailureDoesNotBumpVersion(t *testing.T) {
	c := consoleFixture(t)
	t.Setenv("FANOUT_VALIDATED_POOL_FILE", t.TempDir()+"/missing.json")
	before := c.consoleVersion()
	if _, err := c.m.RefreshNodes(); err == nil {
		t.Fatal("缺失候选池时期望 RefreshNodes 报错")
	}
	if c.consoleVersion() != before {
		t.Fatal("RefreshNodes 失败时版本号不应递增")
	}
}
