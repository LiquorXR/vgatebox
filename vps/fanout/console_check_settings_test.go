package main

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCheckIPTypeContextDefaultsOnAndCanBeDisabled(t *testing.T) {
	if !checkIPTypeFromContext(context.Background()) {
		t.Fatal("candidate checks must check IP type by default")
	}
	ctx := context.WithValue(context.Background(), checkIPTypeContextKey{}, false)
	if checkIPTypeFromContext(ctx) {
		t.Fatal("candidate check type switch was ignored")
	}
}

// 诊断 openvpn 起不来时不要把环境问题记成远端不可达，
// 否则整池失败时无法区分是本机目录/挂载问题还是节点问题。
func TestCheckFailureMapsEnvironmentWriteErrors(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		err  string
		want string
	}{
		{"写配置失败: open /tmp/x/run/f.ovpn: no such file or directory", "probe_environment_failed"},
		{"写凭据失败: open /tmp/x/run/auth.txt: no such file or directory", "probe_environment_failed"},
		{"建运行目录失败: mkdir /tmp/x/run: permission denied", "probe_environment_failed"},
		{"openvpn 提前退出，详见 /tmp/x.log", "openvpn_failed"},
		{"等待 tun0 就绪超时，详见 /tmp/x.log", "openvpn_timeout"},
		{"偶发网络错误", "https_unreachable"},
	} {
		if got := checkFailure(errors.New(tc.err), ctx); got != tc.want {
			t.Fatalf("checkFailure(%q) = %q, want %q", tc.err, got, tc.want)
		}
	}
}

func TestCheckSettingsTTLDefaultsPersistAndValidate(t *testing.T) {
	c := consoleFixture(t)
	w := httptest.NewRecorder()
	c.apiCheckSettings(w, httptest.NewRequest("GET", "/", nil))
	body := w.Body.String()
	if w.Code != 200 || strings.Contains(body, "check_result_ttl_minutes") || !strings.Contains(body, `"check_result_ttl_effective_minutes":30`) ||
		!strings.Contains(body, `"observation_ttl_minutes":30`) || !strings.Contains(body, `"ippure_ttl_hours":12`) {
		t.Fatalf("wrong TTL defaults: %s", body)
	}
	w = httptest.NewRecorder()
	c.apiCheckSettings(w, httptest.NewRequest("POST", "/", strings.NewReader(`{"parallelism":2,"ip_type_check":false,"observation_ttl_minutes":60,"ippure_ttl_hours":12}`)))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"check_result_ttl_effective_minutes":30`) || !strings.Contains(w.Body.String(), `"observation_ttl_effective_minutes":30`) {
		t.Fatalf("TTL settings were not saved: %d %s", w.Code, w.Body.String())
	}
	reloaded, err := newConsole(NewManager(maximumSlots, c.m.workDir))
	if err != nil {
		t.Fatal(err)
	}
	defer reloaded.cancel()
	reloaded.ttlMu.RLock()
	observation, ippure := reloaded.observationTTLMinutes, reloaded.ippureTTLHours
	reloaded.ttlMu.RUnlock()
	if observation != 60 || ippure != 12 {
		t.Fatalf("TTL settings did not survive restart: %d %d", observation, ippure)
	}
	reloaded.m.mu.RLock()
	managerObservationTTL := reloaded.m.observationTTLValueLocked()
	reloaded.m.mu.RUnlock()
	if managerObservationTTL != 30*time.Minute {
		t.Fatalf("manager observation TTL: %s", managerObservationTTL)
	}
	for _, body := range []string{
		`{"parallelism":0}`,
		`{"parallelism":2,"ip_type_check":false,"observation_ttl_minutes":0}`,
		`{"parallelism":2,"ip_type_check":false,"ippure_ttl_hours":0}`,
		`{"parallelism":2,"ip_type_check":false,"ippure_ttl_hours":169}`,
		`{"parallelism":2,"ip_type_check":false,"check_result_ttl_minutes":120}`,
	} {
		w = httptest.NewRecorder()
		c.apiCheckSettings(w, httptest.NewRequest("POST", "/", strings.NewReader(body)))
		if w.Code != 400 {
			t.Fatalf("invalid TTL accepted: %s", body)
		}
	}
}
