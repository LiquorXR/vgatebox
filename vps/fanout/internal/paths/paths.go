// Package paths 是全部落盘路径的唯一出口。
//
// 统一目录（P3-2 已切换）：workDir 为根，其下按用途分子目录。
// 调用方只传 workDir（或任务目录），永远不要自己拼固定文件名。
//
//	workDir/
//	  state/    全部 JSON 状态与期刊：state、native、settings/password/basepath、
//	            console、checks、automation、sstp、third-party、slot_health、ip_type、netbase
//	  cache/    画像缓存：intelligence/{cache,sstp-cache,socks5-cache}.json、country/cache.json
//	  certs/    自签证书
//	  run/      运行态：<ns>.ovpn、auth.txt、*.log、xray.pid、.xray-candidate-*、
//	            .connectivity-* 诊断目录、sstp 任务文件
//	  lock      实例锁（留根：跨版本锁语义，新旧程序互斥）
//
// pool 输出目录与 /etc 配置是外部位置，保持不动（见 PoolFile/PoolConfig/IntelEnv）。
// 任务目录（诊断、sstp 探测）由调用方传参， Diagnose* / SlotOVPN 等函数对任意目录生效。
package paths

import (
	"os"
	"path/filepath"
	"strings"
)

// 子目录名。改名即搬迁：调用方传的永远是 workDir。
const (
	StateDirName = "state"
	CacheDirName = "cache"
	CertsDirName = "certs"
	RunDirName   = "run"
)

func StateDir(root string) string { return filepath.Join(root, StateDirName) }
func CacheDir(root string) string { return filepath.Join(root, CacheDirName) }
func CertsDir(root string) string { return filepath.Join(root, CertsDirName) }
func RunDir(root string) string   { return filepath.Join(root, RunDirName) }

func IntelligenceDir(root string) string {
	return filepath.Join(CacheDir(root), "intelligence")
}

// Ensure 建好 workDir 及固定子目录（0700）。幂等，已存在不报错。
// 入口（main）与各 loader（newConsole/openNative/loadWebSettings）在启动与
// 测试夹具里调用，保证后续原子写（其临时文件建在目标同目录）有父目录可用。
func Ensure(root string) error {
	for _, dir := range []string{root, StateDir(root), CacheDir(root), IntelligenceDir(root), CertsDir(root), RunDir(root)} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
	}
	return nil
}

// Join 拼 workDir 下的顶层文件。过渡期用；布局切换后各函数改走子目录。
func Join(root, name string) string { return filepath.Join(root, name) }

// --- 管理身份（settings.json / password / basepath + 期刊） ---

func Settings(root string) string { return filepath.Join(StateDir(root), "settings.json") }
func Password(root string) string { return filepath.Join(StateDir(root), "password") }
func BasePath(root string) string { return filepath.Join(StateDir(root), "basepath") }
func ManagementJournal() string   { return ".management-transaction.json" }
func ManagementFiles() []string   { return []string{"settings.json", "password", "basepath"} }

// --- Slot 意图 ---

func State(root string) string    { return filepath.Join(StateDir(root), "state.json") }
func StateJournal() string        { return ".state-transaction.json" }
func StateFiles() []string        { return []string{"state.json"} }
func NetBase(root string) string  { return filepath.Join(StateDir(root), "netbase") }
func WorkLock(root string) string { return filepath.Join(root, "lock") }

// --- Xray 配置 ---

func Native(root string) string         { return filepath.Join(StateDir(root), "native.json") }
func NativePrevious(root string) string { return filepath.Join(StateDir(root), "native.previous.json") }
func Xray(root string) string           { return filepath.Join(StateDir(root), "xray.json") }
func NativeJournal() string             { return ".native-transaction.json" }
func NativeFiles() []string             { return []string{"native.json", "xray.json", "native.previous.json"} }
func XrayLog(root string) string        { return filepath.Join(RunDir(root), "xray.log") }
func XrayPID(root string) string        { return filepath.Join(RunDir(root), "xray.pid") }

// --- 控制台计划与检测 ---

func Console(root string) string       { return filepath.Join(StateDir(root), "console.json") }
func ConsoleJournal() string           { return ".console-transaction.json" }
func ConsoleFiles() []string           { return []string{"console.json"} }
func Checks(root string) string        { return filepath.Join(StateDir(root), "checks.json") }
func CheckSettings(root string) string { return filepath.Join(StateDir(root), "check-settings.json") }
func Automation(root string) string    { return filepath.Join(StateDir(root), "automation.json") }
func CheckSchedule(root string) string {
	return filepath.Join(StateDir(root), "check-schedule-state.json")
}
func RefreshState(root string) string { return filepath.Join(StateDir(root), "refresh-state.json") }

// ManualRefreshMarker records a CLI manual refresh that the scheduler has not
// seen yet. Consuming it marks the pool generation covered without starting
// an automatic check, so manual refreshes never trigger one.
func ManualRefreshMarker(root string) string {
	return filepath.Join(StateDir(root), "manual-refresh.json")
}
func SSTP(root string) string { return filepath.Join(StateDir(root), "sstp.json") }
func ThirdPartyChecks(root string) string {
	return filepath.Join(StateDir(root), "third-party-checks.json")
}

// Diagnostic 是任务目录里的检测资源记录（随任务目录走，一般在 run/ 下，
// 与 SlotOVPN 同类；从不是 state/ 下的固定文件）。
func Diagnostic(dir string) string { return filepath.Join(dir, "diagnostic.json") }

// --- 画像与健康证据 ---

func SlotHealth(root string) string  { return filepath.Join(StateDir(root), "slot_health.json") }
func IPTypeCache(root string) string { return filepath.Join(StateDir(root), "ip-type-cache.json") }

// LegacyIPTypeCache is the pre-rename cache file, read once for migration.
func LegacyIPTypeCache(root string) string { return filepath.Join(StateDir(root), "ippure-cache.json") }
func IntelCacheOpenVPN(root string) string { return filepath.Join(IntelligenceDir(root), "cache.json") }
func IntelCacheSSTP(root string) string {
	return filepath.Join(IntelligenceDir(root), "sstp-cache.json")
}
func IntelCacheSOCKS5(root string) string {
	return filepath.Join(IntelligenceDir(root), "socks5-cache.json")
}

// --- 运行态（隧道 ovpn/凭据/日志，netns 名拼在调用方；dir 一般是 run/ 或任务目录） ---

func SlotOVPN(dir, ns string) string { return filepath.Join(dir, ns+".ovpn") }
func SlotAuth(dir string) string     { return filepath.Join(dir, "auth.txt") }
func SlotLog(dir, ns string) string  { return filepath.Join(dir, ns+".log") }

// SSTPLog 是 sstp 探测任务目录里的固定日志名（随任务目录走，一般在 run/ 下）。
func SSTPLog(dir string) string { return filepath.Join(dir, "sstp.log") }

// PanelMode 是旧版外部后端选择文件的哨兵，只读用于拒绝危险升级，
// 永远留在 workDir 根（老安装的值就在那里；搬走会漏检“新旧面板并存”）。
func PanelMode(root string) string { return filepath.Join(root, "panel_mode") }

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

// DBFile 是 SQLite 库文件（state/ 下单文件 + -wal/-shm）。
// backing 三类自有数据：检测结果、（后续）观察快照与画像缓存。
// 意图类文件因 Python 侧直接读 JSON 而留在文件，见 ADR-0007。
func DBFile(root string) string { return filepath.Join(StateDir(root), "vgatebox.db") }

func envOr(name, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v
	}
	return fallback
}
