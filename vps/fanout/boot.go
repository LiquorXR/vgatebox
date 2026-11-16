package main

// boot.go：启动阶段。每个阶段只依赖前一个阶段的产物，便于按顺序测试：
//
//	parseBootConfig → validateBootConfig → bootstrapHost → initControl →
//	startBackground → initHTTP → serveUntilSignal
//
// 这里的拆分不改变原有启动顺序、日志文案或退出码；外部行为由
// docs/adr/0009-boot-phases.md 记录，回归见 boot_test.go。
import (
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"fanout/internal/paths"
)

// bootConfig 是 runFanout 的完整输入：命令行、环境回退与派生标志。
type bootConfig struct {
	webPort      int
	maxSlots     int
	workDir      string
	panelMode    string
	publicIP     string
	showVersion  bool
	checkState   bool
	portExplicit bool
}

// parseBootConfig 只做 flag 解析，不校验业务范围、不触碰文件系统。
// 返回 ok=false 时调用方直接返回 exitCode（-h 为 0，解析失败为 2）。
func parseBootConfig(args []string, stderr io.Writer) (bootConfig, int, bool) {
	fs := flag.NewFlagSet("fanout", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var (
		webPort  = fs.Int("web", 8899, "Web 管理端口")
		maxSlots = fs.Int("max", maximumSlots, "管理员设置的出口容量（默认使用实例地址容量）")
		workDir  = fs.String("dir", "/var/lib/vgatebox/fanout", "工作目录")
	)
	panelMode := fs.String("panel", "", "节点链接后端：仅支持 native（内置 Xray）；空值等同 native")
	publicIP := fs.String("ip", "", "母机公网 IPv4，用于分享链接/SOCKS5 地址；留空则自动探测")
	showVersion := fs.Bool("version", false, "显示版本后退出")
	checkState := fs.Bool("check-state", false, "只读检查内置模式状态副本的迁移配置，不启动服务")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return bootConfig{}, 0, false
		}
		return bootConfig{}, 2, false
	}

	cfg := bootConfig{
		webPort:     *webPort,
		maxSlots:    *maxSlots,
		workDir:     *workDir,
		panelMode:   *panelMode,
		publicIP:    *publicIP,
		showVersion: *showVersion,
		checkState:  *checkState,
	}
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "web" {
			cfg.portExplicit = true
		}
	})
	return cfg, 0, true
}

// validateBootConfig 复现原启动顺序中的业务校验：先校验出口容量，
// 再补 FANOUT_PUBLIC_IP 回退，最后才处理 -version / -check-state。
func validateBootConfig(cfg *bootConfig) int {
	if cfg.maxSlots < 1 || cfg.maxSlots > maximumSlots {
		log.Printf("出口容量范围为 1-%d", maximumSlots)
		return 1
	}
	if cfg.publicIP == "" {
		cfg.publicIP = os.Getenv("FANOUT_PUBLIC_IP")
	}
	return 0
}

// logIntelligenceProvider 是纯启动提示，不参与任何决策。
func logIntelligenceProvider() {
	switch intelligenceProvider() {
	case "off":
		log.Printf("IP 类型画像：已关闭")
	case "custom":
		log.Printf("IP 类型画像：自定义模板已启用")
	default:
		if ippureEnabled() {
			log.Printf("IP 类型画像：IPPure 已启用")
		}
	}
}

// hostRuntime 持有母机级资源：工作目录锁与内置 Xray 进程。
// close 幂等：正常退出、serve 失败与信号处理可能竞争。
type hostRuntime struct {
	unlock func()
	once   sync.Once
}

func (h *hostRuntime) close() {
	if h == nil {
		return
	}
	h.once.Do(func() {
		closePanel()
		if h.unlock != nil {
			h.unlock()
		}
	})
}

// bootstrapHost 完成启动前的母机准备，顺序不能调整：
// root 检查 → 工作目录 → 记录母机 netns → 实例锁 → 实例标识 →
// 管理配置事务恢复 → 公网 IP 预热 → 系统准备 → 面板配置。
func bootstrapHost(cfg bootConfig) (host *hostRuntime, err error) {
	if os.Geteuid() != 0 {
		return nil, fmt.Errorf("需要 root 权限（要创建 netns 和改 iptables）")
	}
	if err := paths.Ensure(cfg.workDir); err != nil {
		return nil, fmt.Errorf("创建工作目录失败: %w", err)
	}
	// 先记下母机的网络命名空间，后面所有子进程都从这里起。
	// 必须赶在建任何隧道之前，那之后线程就可能被带进隧道里了。
	if err := initMainNetns(); err != nil {
		return nil, err
	}

	// 同一个工作目录只许跑一个实例：两份会共用 state.json 互相覆盖。
	unlock, err := lockWorkDir(cfg.workDir)
	if err != nil {
		return nil, err
	}
	success := false
	defer func() {
		if !success {
			unlock()
		}
	}()

	if err := initInstance(cfg.workDir); err != nil {
		return nil, fmt.Errorf("初始化实例标识失败: %w", err)
	}
	if instTag != "" {
		log.Printf("非默认工作目录，本实例用 netns fo%s* 与网段 10.%d.x", instTag, instBase)
	}

	if err := recoverConfigTransaction(paths.StateDir(cfg.workDir), managementJournal, managementConfigFiles); err != nil {
		return nil, fmt.Errorf("恢复管理配置失败: %w", err)
	}
	setPublicIPOverride(cfg.publicIP)
	go hostPublicIP() // 预热探测，别让首个请求阻塞
	if err := prepareHost(); err != nil {
		return nil, err
	}

	if err := configurePanel(cfg.workDir, cfg.panelMode); err != nil {
		return nil, fmt.Errorf("节点链接后端配置失败: %w", err)
	}
	logStartupPanel()

	success = true
	return &hostRuntime{unlock: unlock}, nil
}

// logStartupPanel 只报告内置后端状态，不阻断启动：
// Xray 暂不可用时 Web 界面仍可显示原因并让用户重试。
func logStartupPanel() {
	p, err := openPanel()
	if err != nil {
		log.Printf("节点链接后端暂不可用（可在 Web 界面查看原因）: %v", err)
		return
	}
	log.Printf("节点链接后端: %s", p.Describe())
}

// controlRuntime 是控制面：Manager 维护隧道，Console 维护意图与检测。
type controlRuntime struct {
	mgr     *Manager
	console *Console
}

// initControl 复现原启动顺序：Manager → 首次候选拉取 → Console →
// 临时检测清理 → 状态恢复 → 首装 Slot → 内置接入恢复。
func initControl(cfg bootConfig) (*controlRuntime, error) {
	mgr := NewManager(cfg.maxSlots, cfg.workDir)
	// 换节点 worker 必须在状态恢复前就绪：restoreState 会拉起隧道，
	// 发布成功后可能立刻排队下一次换节点。
	mgr.startRotationWorker()
	log.Printf("正在拉取节点列表...")
	if n, err := mgr.RefreshNodes(); err != nil {
		log.Printf("拉取失败（可在 Web 界面重试）: %v", err)
	} else {
		log.Printf("已获取 %d 个节点", n)
	}

	console, err := newConsole(mgr)
	if err != nil {
		return nil, fmt.Errorf("加载出口计划失败: %w", err)
	}
	if err := console.cleanupDiagnostics(); err != nil {
		return nil, fmt.Errorf("临时检测清理失败: %w", err)
	}
	if n, err := mgr.restoreState(); err != nil {
		return nil, fmt.Errorf("恢复上次状态失败，拒绝忽略已保存策略: %w", err)
	} else if n > 0 {
		log.Printf("正在恢复上次的 %d 条隧道", n)
	}
	startConfiguredSlot(mgr)
	// Direct and third-party entries must restart even when there are no VPN
	// Gate Slots to trigger an outbound reconciliation after process restart.
	if p, err := openPanel(); err == nil {
		if err := p.OnTunnelsChanged(mgr.Tunnels()); err != nil {
			return nil, fmt.Errorf("恢复内置接入失败: %w", err)
		}
	}
	return &controlRuntime{mgr: mgr, console: console}, nil
}

// startBackground 启动常驻 worker。顺序不影响正确性，但保持与原实现一致。
func (c *controlRuntime) startBackground() {
	go c.console.watch()
	go c.console.watchAutomation()
	go c.console.watchChecksPersistence()
	go c.console.sstp.watch()

	go c.mgr.WatchHealth(c.console.ctx)
	go c.mgr.WatchNodePool()
}

// httpRuntime 只暴露阻塞服务所需的 webServer。
type httpRuntime struct {
	server *webServer
}

// registerRoutes 注册全部 HTTP 路由。路由表本身由 routes.go +
// TestAPIContract 锁定；这里只决定注册顺序，不包含鉴权与中间件。
func registerRoutes(mux *http.ServeMux, ctl *controlRuntime) {
	mux.HandleFunc("/", handleIndex)
	ctl.console.routes(mux)
	mux.HandleFunc("/api/nodes", apiNodes(ctl.mgr))
	mux.HandleFunc("/api/stop", postOnly(apiStop(ctl.mgr)))
	mux.HandleFunc("/api/swap", postOnly(apiSwap(ctl.mgr)))
	mux.HandleFunc("/api/slot/policy", apiSlotPolicy(ctl.mgr))
	mux.HandleFunc("/api/slot/policy/rollback", apiRollbackSlotPolicy(ctl.mgr))
	mux.HandleFunc("/api/config/rollback", apiRollbackNative(ctl.mgr))
	mux.HandleFunc("/api/slot/resume", apiSlotLifecycle(ctl.mgr, false))
	// Old compatibility route: it deletes the Slot without updating the
	// console target. The new console uses /api/console/slot/delete instead.
	mux.HandleFunc("/api/slot/delete", apiSlotLifecycle(ctl.mgr, true))
	mux.HandleFunc("/api/cred", postOnly(apiCred(ctl.mgr)))
	mux.HandleFunc("/api/regions", apiRegions(ctl.mgr))
	mux.HandleFunc("/api/provision", postOnly(apiProvision(ctl.mgr)))
	mux.HandleFunc("/api/jobs", apiJobs(ctl.mgr))
	mux.HandleFunc("/api/jobs/dismiss", postOnly(apiJobDismiss(ctl.mgr)))
	mux.HandleFunc("/api/exits", apiExits(ctl.mgr))
	mux.HandleFunc("/api/export/xray", apiExportXray(ctl.mgr))
	mux.HandleFunc("/api/xui/inbounds", apiXUIInbounds(ctl.mgr))
	mux.HandleFunc("/api/xui/bind", postOnly(apiXUIBind(ctl.mgr)))
	mux.HandleFunc("/api/xui/clone", postOnly(apiXUIClone(ctl.mgr)))
	mux.HandleFunc("/api/xui/detail", apiXUIDetail)
	mux.HandleFunc("/api/xui/links", apiXUILinks)
	mux.HandleFunc("/api/xui/delete", postOnly(apiXUIDelete(ctl.mgr)))
	mux.HandleFunc("/api/xui/encryption/rotate", postOnly(apiXUIRotateEncryption(ctl.mgr)))
	mux.HandleFunc("/api/xui/encryption/set", postOnly(apiXUISetEncryption(ctl.mgr)))
	mux.HandleFunc("/api/panel/inbound/new", postOnly(apiInboundCreate(ctl.mgr)))
	mux.HandleFunc("/api/panel/inbound/update", postOnly(apiInboundUpdate(ctl.mgr)))
	mux.HandleFunc("/api/panel/client/add", postOnly(apiClientAdd(ctl.mgr)))
	mux.HandleFunc("/api/panel/client/del", postOnly(apiClientDelete(ctl.mgr)))
	mux.HandleFunc("/api/panel/client/reset", postOnly(apiClientReset(ctl.mgr)))
	mux.HandleFunc("/api/sub", apiSub)
	mux.HandleFunc("/api/sub/reset", apiSubReset)
	// 订阅本体走免登录（见 auth.go 的放行），口令在 handleSub 里验
	mux.HandleFunc("/sub", handleSub(ctl.mgr))
	// 探活走免登录（见 auth.go 的放行）：只返回计数与版本等非敏感字段，见 healthz.go
	mux.HandleFunc("/healthz", handleHealthz(ctl.mgr))
}

// initHTTP 装配鉴权、访问路径与中间件，并在 server 建立后注册
// 需要 auth/srv 的设置与更新路由。
func initHTTP(cfg bootConfig, ctl *controlRuntime) (*httpRuntime, error) {
	mux := http.NewServeMux()
	registerRoutes(mux, ctl)

	auth, created, err := NewAuth(cfg.workDir)
	if err != nil {
		return nil, fmt.Errorf("初始化访问口令失败: %w", err)
	}
	if created {
		log.Printf("已生成访问口令，见 %s", paths.Password(cfg.workDir))
	}

	bpCreated, err := initBasePath(cfg.workDir)
	if err != nil {
		return nil, fmt.Errorf("初始化访问路径失败: %w", err)
	}
	if bpCreated {
		log.Printf("已生成访问路径，见 %s", paths.BasePath(cfg.workDir))
	}

	handler := withRequestID(StripBasePath(auth.Wrap(mux)))
	handler = withStatusLog(withSecurityHeaders(withRecovery(withGzip(handler))))
	srv := newWebServer(handler)
	// 设置面板：改密码 / 改路径 / 改端口 / 改本地监听。
	mux.HandleFunc("/api/settings", apiSettings(auth, srv))
	updater := newUpdateManager(cfg.workDir)
	mux.HandleFunc("/api/update/check", updater.check)
	mux.HandleFunc("/api/update/apply", updater.apply)
	mux.HandleFunc("/api/update/status", updater.status)

	return &httpRuntime{server: srv}, nil
}

// logManagementEndpoint 打印用户首次登录所需的信息，必须在 basepath 初始化之后调用。
func logManagementEndpoint(webCfg WebSettings) {
	log.Printf("管理界面: http://<本机IP>%s%s/", webCfg.listenAddrString(), currentBasePath())
	if base, fixed, err := fixedSlotPort(1); fixed && err == nil {
		log.Printf("Exit Slot 固定端口基址为 %d（slot 1 使用 127.0.0.1:%d）", base, base)
	} else {
		log.Printf("SOCKS5 端口在 %d-%d 之间随机分配", randPortMin, randPortMax)
	}
}

// installShutdownHandler 在后台 worker 启动后安装信号处理，
// 与原启动顺序一致：即使鉴权/访问路径初始化期间收到 SIGTERM，
// 也会先清理隧道、关闭 SQLite WAL 并释放实例锁。
func installShutdownHandler(ctl *controlRuntime, host *hostRuntime) {
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-stop
		log.Println("正在清理所有隧道...")
		ctl.console.shutdown()
		ctl.mgr.Shutdown()
		ctl.console.closeStores()
		host.close()
		os.Exit(0)
	}()
}

// serveUntilSignal 永久阻塞在 HTTP 监听上。
// serve 只在启动失败时返回，因此返回即错误。
func serveUntilSignal(srv *webServer) int {
	log.Print(srv.serve())
	return 1
}
