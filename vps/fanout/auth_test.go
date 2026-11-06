package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestAuth() *Auth {
	return &Auth{
		password: "secret",
		sessions: map[string]time.Time{},
		fails:    map[string]*loginFails{},
	}
}

// 连续失败达到阈值后，该 IP 应被冷却挡下。
func TestLoginThrottleBlocksAfterMaxFails(t *testing.T) {
	a := newTestAuth()
	const ip = "203.0.113.7"

	for i := 0; i < loginMaxFails; i++ {
		if a.blocked(ip) {
			t.Fatalf("第 %d 次失败前不该被封", i)
		}
		a.recordFail(ip)
	}
	if !a.blocked(ip) {
		t.Fatalf("连续 %d 次失败后应进入冷却", loginMaxFails)
	}
}

// 登录成功要清零，之前的失败不该累积到下一轮。
func TestLoginThrottleClearOnSuccess(t *testing.T) {
	a := newTestAuth()
	const ip = "203.0.113.8"

	for i := 0; i < loginMaxFails-1; i++ {
		a.recordFail(ip)
	}
	a.clearFails(ip)
	if a.blocked(ip) {
		t.Fatal("清零后不该被封")
	}
	// 清零后再错一次也不该立刻触发冷却
	a.recordFail(ip)
	if a.blocked(ip) {
		t.Fatal("清零后单次失败不该被封")
	}
}

// 不同 IP 的失败互不牵连。
func TestLoginThrottleIsolatesIPs(t *testing.T) {
	a := newTestAuth()
	for i := 0; i < loginMaxFails; i++ {
		a.recordFail("198.51.100.1")
	}
	if a.blocked("198.51.100.2") {
		t.Fatal("一个 IP 被封不该波及另一个 IP")
	}
}

// 冷却到期后应自动解封。
func TestLoginThrottleUnblocksAfterExpiry(t *testing.T) {
	a := newTestAuth()
	const ip = "203.0.113.9"
	for i := 0; i < loginMaxFails; i++ {
		a.recordFail(ip)
	}
	if !a.blocked(ip) {
		t.Fatal("应先进入冷却")
	}
	// 手动把封禁时间拨到过去，模拟冷却结束
	a.mu.Lock()
	a.fails[ip].blocked = time.Now().Add(-time.Second)
	a.mu.Unlock()
	if a.blocked(ip) {
		t.Fatal("冷却到期后应解封")
	}
}

// 新口令至少 12 位；已落盘的短口令仍可登录，只在下次修改时执行新规。
func TestSetPasswordEnforcesMinLength(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "password"), []byte("oldshort1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	a, created, err := NewAuth(dir)
	if err != nil || created {
		t.Fatalf("NewAuth = %v, created = %v, err = %v", a, created, err)
	}
	if !a.check("oldshort1") {
		t.Fatal("已有短口令应仍可登录")
	}
	if err := a.SetPassword("eleven-char"); err == nil {
		t.Fatal("11 位新口令应被拒绝")
	}
	if !a.check("oldshort1") {
		t.Fatal("拒绝后旧口令必须保留")
	}
	if err := a.SetPassword("twelve-chars"); err != nil {
		t.Fatalf("12 位新口令应被接受: %v", err)
	}
	if !a.check("twelve-chars") || a.check("oldshort1") {
		t.Fatal("新口令未生效或旧口令未失效")
	}
}

// /api/settings 改密码同样执行 12 位下限，拒绝时配置与口令都不动。
func TestSettingsRejectsShortPassword(t *testing.T) {
	auth, srv, _ := managementFixture(t)
	keep, err := auth.issue()
	if err != nil {
		t.Fatal(err)
	}
	body := `{"password":"short-11cha","current_password":"fixture-old-password"}`
	r := httptest.NewRequest(http.MethodPost, "/api/settings", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.AddCookie(&http.Cookie{Name: sessionCookie, Value: keep})
	rec := httptest.NewRecorder()
	apiSettings(auth, srv)(rec, r)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("11 位密码 status = %d，想要 400", rec.Code)
	}
	if !auth.check("fixture-old-password") {
		t.Fatal("拒绝后旧口令必须保留")
	}
}
