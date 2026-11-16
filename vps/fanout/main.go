package main

// main.go：进程入口与启动编排；参数解析、特权引导、控制面与 HTTP 装配见 boot.go。
import (
	"fanout/internal/paths"
	"fmt"
	"io"
	"log"
	"os"
)

// version 由构建时通过 -ldflags 注入。
var version = "dev"

func main() {
	os.Exit(runFanout(os.Args[1:], os.Stdout, os.Stderr))
}

// runFanout 按固定顺序调用各启动阶段，返回进程退出码。
// main 只剩参数透传；各阶段可在 boot_test.go 中单独测试，
// 启动顺序与退出码由 TestBoot* 固定，避免拆分时改变外部行为。
func runFanout(args []string, stdout, stderr io.Writer) int {
	initStructuredLogging()
	loadEnvFile(paths.IntelEnv())

	cfg, code, ok := parseBootConfig(args, stderr)
	if !ok {
		return code
	}
	logIntelligenceProvider()
	if code := validateBootConfig(&cfg); code != 0 {
		return code
	}
	if cfg.showVersion {
		fmt.Fprintln(stdout, "vgatebox", version)
		return 0
	}
	if cfg.checkState {
		return runMigrationCheck(stdout, paths.StateDir(cfg.workDir), cfg.maxSlots, cfg.webPort)
	}

	host, err := bootstrapHost(cfg)
	if err != nil {
		log.Print(err)
		return 1
	}
	defer host.close()

	webCfg, err := loadWebSettings(cfg.workDir, cfg.webPort, cfg.portExplicit)
	if err != nil {
		log.Printf("加载 Web 设置失败: %v", err)
		return 1
	}

	ctl, err := initControl(cfg)
	if err != nil {
		log.Print(err)
		return 1
	}
	ctl.startBackground()
	// 与原启动顺序一致：后台 worker 起来后立刻安装信号处理，
	// 这样鉴权/访问路径初始化期间的 SIGTERM 也会走完整清理。
	installShutdownHandler(ctl, host)

	httpRt, err := initHTTP(cfg, ctl)
	if err != nil {
		log.Print(err)
		return 1
	}
	logManagementEndpoint(webCfg)
	return serveUntilSignal(httpRt.server)
}
