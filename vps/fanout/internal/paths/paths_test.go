package paths

import (
	"os"
	"path/filepath"
	"testing"
)

// 落盘路径只许从这里拼：调用方不得再出现 filepath.Join(dir, "<固定名>")。
// 本测试锁死统一目录映射：改名即搬迁，调用方不动。
func TestAccessorsResolveUnderSubdirs(t *testing.T) {
	root := t.TempDir()
	run := RunDir(root)
	cases := map[string]string{
		"state/settings.json":                  Settings(root),
		"state/password":                       Password(root),
		"state/basepath":                       BasePath(root),
		"state/state.json":                     State(root),
		"state/netbase":                        NetBase(root),
		"state/native.json":                    Native(root),
		"state/native.previous.json":           NativePrevious(root),
		"state/xray.json":                      Xray(root),
		"state/console.json":                   Console(root),
		"state/checks.json":                    Checks(root),
		"state/check-settings.json":            CheckSettings(root),
		"state/automation.json":                Automation(root),
		"state/check-schedule-state.json":      CheckSchedule(root),
		"state/refresh-state.json":             RefreshState(root),
		"state/manual-refresh.json":            ManualRefreshMarker(root),
		"state/sstp.json":                      SSTP(root),
		"state/third-party-checks.json":        ThirdPartyChecks(root),
		"state/slot_health.json":               SlotHealth(root),
		"state/ippure-cache.json":              IPPureCache(root),
		"run/xray.log":                         XrayLog(root),
		"run/xray.pid":                         XrayPID(root),
		"lock":                                 WorkLock(root),
		"cache/intelligence/cache.json":        IntelCacheOpenVPN(root),
		"cache/intelligence/sstp-cache.json":   IntelCacheSSTP(root),
		"cache/intelligence/socks5-cache.json": IntelCacheSOCKS5(root),
		"certs":                                CertsDir(root),
	}
	for rel, got := range cases {
		if want := filepath.Join(root, filepath.FromSlash(rel)); got != want {
			t.Errorf("%s = %q，想要 %q", rel, got, want)
		}
	}
	if got := SlotOVPN(run, "fo1"); got != filepath.Join(run, "fo1.ovpn") {
		t.Errorf("slot ovpn = %q", got)
	}
	if got := SlotAuth(run); got != filepath.Join(run, "auth.txt") {
		t.Errorf("slot auth = %q", got)
	}
	if got := SlotLog(run, "fo1"); got != filepath.Join(run, "fo1.log") {
		t.Errorf("slot log = %q", got)
	}
	if got := SSTPLog(run); got != filepath.Join(run, "sstp.log") {
		t.Errorf("sstp log = %q", got)
	}
	if got := Diagnostic(run); got != filepath.Join(run, "diagnostic.json") {
		t.Errorf("diagnostic = %q", got)
	}
}

func TestEnsureCreatesDirs(t *testing.T) {
	root := filepath.Join(t.TempDir(), "work")
	if err := Ensure(root); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{root, StateDir(root), CacheDir(root), IntelligenceDir(root), CertsDir(root), RunDir(root)} {
		st, err := os.Stat(dir)
		if err != nil || !st.IsDir() {
			t.Fatalf("%s 不存在或不是目录: %v", dir, err)
		}
		if st.Mode().Perm()&0700 != 0700 {
			t.Fatalf("%s 权限不足 0700: %v", dir, st.Mode())
		}
	}
	// 幂等：已存在不报错。
	if err := Ensure(root); err != nil {
		t.Fatal(err)
	}
}

func TestExternalDefaults(t *testing.T) {
	t.Setenv("FANOUT_VALIDATED_POOL_FILE", "")
	t.Setenv("FANOUT_POOL_CONFIG", "")
	t.Setenv("FANOUT_INTELLIGENCE_ENV_FILE", "")
	t.Setenv("FANOUT_TOOLBOX_SCRIPT", "")
	if got := PoolFile(); got != "/var/lib/vgatebox/pool/node_pool.json" {
		t.Errorf("pool = %q", got)
	}
	if got := PoolConfig(); got != "/etc/vgatebox/pool.json" {
		t.Errorf("pool config = %q", got)
	}
	if got := IntelEnv(); got != "/etc/vgatebox/intelligence.env" {
		t.Errorf("intel env = %q", got)
	}
	if got := Toolbox(); got != "/usr/local/lib/vgatebox/scripts/toolbox.py" {
		t.Errorf("toolbox = %q", got)
	}
	t.Setenv("FANOUT_POOL_CONFIG", "/tmp/fixture-pool.json")
	if got := PoolConfig(); got != "/tmp/fixture-pool.json" {
		t.Errorf("env 覆盖不生效: %q", got)
	}
}
