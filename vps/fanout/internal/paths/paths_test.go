package paths

import (
	"os"
	"path/filepath"
	"testing"
)

// 落盘路径只许从这里拼：调用方不得再出现 filepath.Join(dir, "<固定名>")。
func TestAccessorsStayUnderRoot(t *testing.T) {
	root := t.TempDir()
	cases := map[string]string{
		"settings.json":             Settings(root),
		"password":                  Password(root),
		"basepath":                  BasePath(root),
		"state.json":                State(root),
		"netbase":                   NetBase(root),
		"lock":                      WorkLock(root),
		"native.json":               Native(root),
		"native.previous.json":      NativePrevious(root),
		"xray.json":                 Xray(root),
		"xray.log":                  XrayLog(root),
		"xray.pid":                  XrayPID(root),
		"console.json":              Console(root),
		"checks.json":               Checks(root),
		"check-settings.json":       CheckSettings(root),
		"automation.json":           Automation(root),
		"check-schedule-state.json": CheckSchedule(root),
		"refresh-state.json":        RefreshState(root),
		"sstp.json":                 SSTP(root),
		"third-party-checks.json":   ThirdPartyChecks(root),
		"diagnostic.json":           Diagnostic(root),
		"slot_health.json":          SlotHealth(root),
		"ippure-cache.json":         IPPureCache(root),
	}
	for name, got := range cases {
		if filepath.Dir(got) != root || filepath.Base(got) != name {
			t.Errorf("%s = %q，想要 %s 下的同名文件", name, got, root)
		}
	}
	if got := CertsDir(root); filepath.Dir(got) != root || filepath.Base(got) != "certs" {
		t.Errorf("certs = %q", got)
	}
	if got := IntelCacheOpenVPN(root); got != filepath.Join(root, "intelligence", "cache.json") {
		t.Errorf("intel openvpn = %q", got)
	}
	if got := SlotOVPN(root, "fo1"); got != filepath.Join(root, "fo1.ovpn") {
		t.Errorf("slot ovpn = %q", got)
	}
}

func TestEnsureCreatesDirs(t *testing.T) {
	root := filepath.Join(t.TempDir(), "work")
	if err := Ensure(root); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{root, CertsDir(root), IntelligenceDir(root)} {
		st, err := os.Stat(dir)
		if err != nil || !st.IsDir() {
			t.Fatalf("%s 不存在或不是目录: %v", dir, err)
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
