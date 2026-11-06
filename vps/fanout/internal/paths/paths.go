// Package paths 是全部落盘路径的唯一出口。
//
// 背景：持久化文件散在 workDir 顶层（state.json、native.json、password …）、
// intelligence/、certs/ 子目录、pool 输出目录与 /etc 配置里，
// 同一个文件名在生产代码与测试里被 filepath.Join 重复拼几十次，
// 改名或搬目录必然漏改。之后所有新增落盘点必须先加到这里。
//
// 统一目录（P3）：布局切换时只改本文件的映射函数，调用方不动。
// 目标布局（workDir 为根）：state/ 放全部 JSON 状态与期刊，
// cache/ 放画像缓存，certs/ 放证书，run/ 放 ovpn/log/pid 与诊断目录，
// lock 留在根（跨版本锁语义）；pool 与 /etc 配置保持外部位置。
package paths

import (
	"os"
	"path/filepath"
	"strings"
)

// Join 拼 workDir 下的顶层文件。过渡期用；布局切换后各函数改走子目录。
func Join(root, name string) string { return filepath.Join(root, name) }

// Ensure 建好 workDir 及固定子目录（0700）。幂等，已存在不报错。
func Ensure(root string) error {
	for _, dir := range []string{root, CertsDir(root), IntelligenceDir(root)} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
	}
	return nil
}

func CertsDir(root string) string        { return filepath.Join(root, "certs") }
func IntelligenceDir(root string) string { return filepath.Join(root, "intelligence") }

// --- 管理身份（settings.json / password / basepath + 期刊） ---

func Settings(root string) string { return filepath.Join(root, "settings.json") }
func Password(root string) string { return filepath.Join(root, "password") }
func BasePath(root string) string { return filepath.Join(root, "basepath") }
func ManagementJournal() string   { return ".management-transaction.json" }
func ManagementFiles() []string   { return []string{"settings.json", "password", "basepath"} }

// --- Slot 意图 ---

func State(root string) string    { return filepath.Join(root, "state.json") }
func StateJournal() string        { return ".state-transaction.json" }
func StateFiles() []string        { return []string{"state.json"} }
func NetBase(root string) string  { return filepath.Join(root, "netbase") }
func WorkLock(root string) string { return filepath.Join(root, "lock") }

// --- Xray 配置 ---

func Native(root string) string         { return filepath.Join(root, "native.json") }
func NativePrevious(root string) string { return filepath.Join(root, "native.previous.json") }
func Xray(root string) string           { return filepath.Join(root, "xray.json") }
func NativeJournal() string             { return ".native-transaction.json" }
func NativeFiles() []string             { return []string{"native.json", "xray.json", "native.previous.json"} }
func XrayLog(root string) string        { return filepath.Join(root, "xray.log") }
func XrayPID(root string) string        { return filepath.Join(root, "xray.pid") }

// --- 控制台计划与检测 ---

func Console(root string) string          { return filepath.Join(root, "console.json") }
func ConsoleJournal() string              { return ".console-transaction.json" }
func ConsoleFiles() []string              { return []string{"console.json"} }
func Checks(root string) string           { return filepath.Join(root, "checks.json") }
func CheckSettings(root string) string    { return filepath.Join(root, "check-settings.json") }
func Automation(root string) string       { return filepath.Join(root, "automation.json") }
func CheckSchedule(root string) string    { return filepath.Join(root, "check-schedule-state.json") }
func RefreshState(root string) string     { return filepath.Join(root, "refresh-state.json") }
func SSTP(root string) string             { return filepath.Join(root, "sstp.json") }
func ThirdPartyChecks(root string) string { return filepath.Join(root, "third-party-checks.json") }
func Diagnostic(root string) string       { return filepath.Join(root, "diagnostic.json") }

// --- 画像与健康证据 ---

func SlotHealth(root string) string        { return filepath.Join(root, "slot_health.json") }
func IPPureCache(root string) string       { return filepath.Join(root, "ippure-cache.json") }
func IntelCacheOpenVPN(root string) string { return filepath.Join(root, "intelligence", "cache.json") }
func IntelCacheSSTP(root string) string {
	return filepath.Join(root, "intelligence", "sstp-cache.json")
}
func IntelCacheSOCKS5(root string) string {
	return filepath.Join(root, "intelligence", "socks5-cache.json")
}

// --- 运行态（隧道 ovpn/凭据/日志，netns 名拼在调用方） ---

func SlotOVPN(dir, ns string) string { return filepath.Join(dir, ns+".ovpn") }
func SlotAuth(dir string) string     { return filepath.Join(dir, "auth.txt") }
func SlotLog(dir, ns string) string  { return filepath.Join(dir, ns+".log") }

// --- 外部位置（env 可覆盖，默认值集中于此） ---

func PoolFile() string {
	return envOr("FANOUT_VALIDATED_POOL_FILE", "/var/lib/vgatebox/pool/node_pool.json")
}
func PoolConfig() string { return envOr("FANOUT_POOL_CONFIG", "/etc/vgatebox/pool.json") }
func IntelEnv() string {
	return envOr("FANOUT_INTELLIGENCE_ENV_FILE", "/etc/vgatebox/intelligence.env")
}
func Toolbox() string {
	return envOr("FANOUT_TOOLBOX_SCRIPT", "/usr/local/lib/vgatebox/scripts/toolbox.py")
}
func DeployStatus() string { return "/var/lib/vgatebox-deploy/status.json" }

func envOr(name, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v
	}
	return fallback
}
