package main

// main.go：进程入口与启动流程（runFanout）；API 处理器见 api_slots.go / api_panel.go。
import (
	"fanout/internal/paths"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

// version 由构建时通过 -ldflags 注入。
var version = "dev"

func main() {
	os.Exit(runFanout(os.Args[1:], os.Stdout, os.Stderr))
}

// runFanout 承载原 main 的全部启动流程，返回进程退出码。
// main 只剩参数透传，便于测试 flag 解析与版本/迁移检查等早期退出分支，
// 也是后续继续拆分启动步骤（监听构建、路由注册）的落点。
// 命名避开 tunnel.go 中执行网络配置命令的 run。
func runFanout(args []string, stdout, stderr io.Writer) int {
	initStructuredLogging()
	loadEnvFile(paths.IntelEnv())
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
			return 0
		}
		return 2
	}
	if ippureEnabled() {
		log.Printf("IP 类型画像：IPPure 已启用")
	}
	if *maxSlots < 1 || *maxSlots > maximumSlots {
		log.Printf("出口容量范围为 1-%d", maximumSlots)
		return 1
	}

	if *publicIP == "" {
		*publicIP = os.Getenv("FANOUT_PUBLIC_IP")
	}

	if *showVersion {
		fmt.Fprintln(stdout, "vgatebox", version)
		return 0
	}
	if *checkState {
		return runMigrationCheck(stdout, paths.StateDir(*workDir), *maxSlots, *webPort)
	}

	if os.Geteuid() != 0 {
		log.Print("需要 root 权限（要创建 netns 和改 iptables）")
		return 1
	}
	if err := paths.Ensure(*workDir); err != nil {
		log.Printf("创建工作目录失败: %v", err)
		return 1
	}

	// 先记下母机的网络命名空间，后面所有子进程都从这里起。
	// 必须赶在建任何隧道之前，那之后线程就可能被带进隧道里了
	if err := initMainNetns(); err != nil {
		log.Print(err)
		return 1
	}

	// 同一个工作目录只许跑一个实例：两份会共用 state.json 互相覆盖，隧道记录直接丢
	unlock, err := lockWorkDir(*workDir)
	if err != nil {
		log.Print(err)
		return 1
	}
	defer unlock()

	// 定下这台机器上属于本实例的 netns 名与网段。默认目录沿用老名字，
	// 换了目录就自动隔离，免得两个实例互相拆隧道（见 instance.go）
	if err := initInstance(*workDir); err != nil {
		log.Printf("初始化实例标识失败: %v", err)
		return 1
	}
	if instTag != "" {
		log.Printf("非默认工作目录，本实例用 netns fo%s* 与网段 10.%d.x", instTag, instBase)
	}

	if err := recoverConfigTransaction(*workDir, managementJournal, managementConfigFiles); err != nil {
		log.Printf("恢复管理配置失败: %v", err)
		return 1
	}
	setPublicIPOverride(*publicIP)
	go hostPublicIP() // 预热探测，别让首个请求阻塞
	if err := prepareHost(); err != nil {
		log.Print(err)
		return 1
	}

	if err := configurePanel(*workDir, *panelMode); err != nil {
		log.Printf("节点链接后端配置失败: %v", err)
		return 1
	}
	if p, err := openPanel(); err != nil {
		log.Printf("节点链接后端暂不可用（可在 Web 界面查看原因）: %v", err)
	} else {
		log.Printf("节点链接后端: %s", p.Describe())
	}

	// 用户显式给了 -web 就以命令行为准，否则沿用界面上存过的端口
	portExplicit := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "web" {
			portExplicit = true
		}
	})
	webCfg, err := loadWebSettings(*workDir, *webPort, portExplicit)
	if err != nil {
		log.Printf("加载 Web 设置失败: %v", err)
		return 1
	}

	mgr := NewManager(*maxSlots, *workDir)
	log.Printf("正在拉取节点列表...")
	if n, err := mgr.RefreshNodes(); err != nil {
		log.Printf("拉取失败（可在 Web 界面重试）: %v", err)
	} else {
		log.Printf("已获取 %d 个节点", n)
	}

	console, err := newConsole(mgr)
	if err != nil {
		log.Printf("加载出口计划失败: %v", err)
		return 1
	}
	if err := console.cleanupDiagnostics(); err != nil {
		log.Printf("临时检测清理失败: %v", err)
		return 1
	}
	if n, err := mgr.restoreState(); err != nil {
		log.Printf("恢复上次状态失败，拒绝忽略已保存策略: %v", err)
		return 1
	} else if n > 0 {
		log.Printf("正在恢复上次的 %d 条隧道", n)
	}
	startConfiguredSlot(mgr)
	// Direct and third-party entries must restart even when there are no VPN
	// Gate Slots to trigger an outbound reconciliation after process restart.
	if p, err := openPanel(); err == nil {
		if err := p.OnTunnelsChanged(mgr.Tunnels()); err != nil {
			log.Printf("恢复内置接入失败: %v", err)
			return 1
		}
	}

	go console.watch()
	go console.watchAutomation()
	go console.watchChecksPersistence()
	go console.sstp.watch()

	go mgr.WatchHealth(console.ctx)
	go mgr.WatchNodePool()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-stop
		log.Println("正在清理所有隧道...")
		console.shutdown()
		mgr.Shutdown()
		closePanel()
		unlock() // os.Exit 会绕过 defer，这里手动放锁
		os.Exit(0)
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("/", handleIndex)
	console.routes(mux)
	mux.HandleFunc("/api/nodes", apiNodes(mgr))
	mux.HandleFunc("/api/stop", postOnly(apiStop(mgr)))
	mux.HandleFunc("/api/swap", postOnly(apiSwap(mgr)))
	mux.HandleFunc("/api/slot/policy", apiSlotPolicy(mgr))
	mux.HandleFunc("/api/slot/policy/rollback", apiRollbackSlotPolicy(mgr))
	mux.HandleFunc("/api/config/rollback", apiRollbackNative(mgr))
	mux.HandleFunc("/api/slot/resume", apiSlotLifecycle(mgr, false))
	// Old compatibility route: it deletes the Slot without updating the
	// console target. The new console uses /api/console/slot/delete instead.
	mux.HandleFunc("/api/slot/delete", apiSlotLifecycle(mgr, true))
	mux.HandleFunc("/api/cred", postOnly(apiCred(mgr)))
	mux.HandleFunc("/api/regions", apiRegions(mgr))
	mux.HandleFunc("/api/provision", postOnly(apiProvision(mgr)))
	mux.HandleFunc("/api/jobs", apiJobs(mgr))
	mux.HandleFunc("/api/jobs/dismiss", postOnly(apiJobDismiss(mgr)))
	mux.HandleFunc("/api/exits", apiExits(mgr))
	mux.HandleFunc("/api/export/xray", apiExportXray(mgr))
	mux.HandleFunc("/api/xui/inbounds", apiXUIInbounds(mgr))
	mux.HandleFunc("/api/xui/bind", postOnly(apiXUIBind(mgr)))
	mux.HandleFunc("/api/xui/clone", postOnly(apiXUIClone(mgr)))
	mux.HandleFunc("/api/xui/detail", apiXUIDetail)
	mux.HandleFunc("/api/xui/links", apiXUILinks)
	mux.HandleFunc("/api/xui/delete", postOnly(apiXUIDelete(mgr)))
	mux.HandleFunc("/api/xui/encryption/rotate", postOnly(apiXUIRotateEncryption(mgr)))
	mux.HandleFunc("/api/xui/encryption/set", postOnly(apiXUISetEncryption(mgr)))
	mux.HandleFunc("/api/panel/inbound/new", postOnly(apiInboundCreate(mgr)))
	mux.HandleFunc("/api/panel/inbound/update", postOnly(apiInboundUpdate(mgr)))
	mux.HandleFunc("/api/panel/client/add", postOnly(apiClientAdd(mgr)))
	mux.HandleFunc("/api/panel/client/del", postOnly(apiClientDelete(mgr)))
	mux.HandleFunc("/api/panel/client/reset", postOnly(apiClientReset(mgr)))
	mux.HandleFunc("/api/sub", apiSub)
	mux.HandleFunc("/api/sub/reset", apiSubReset)
	// 订阅本体走免登录（见 auth.go 的放行），口令在 handleSub 里验
	mux.HandleFunc("/sub", handleSub(mgr))
	// 探活走免登录（见 auth.go 的放行）：只返回计数与版本等非敏感字段，见 healthz.go
	mux.HandleFunc("/healthz", handleHealthz(mgr))

	auth, created, err := NewAuth(*workDir)
	if err != nil {
		log.Printf("初始化访问口令失败: %v", err)
		return 1
	}
	if created {
		log.Printf("已生成访问口令，见 %s", paths.Password(*workDir))
	}

	bpCreated, err := initBasePath(*workDir)
	if err != nil {
		log.Printf("初始化访问路径失败: %v", err)
		return 1
	}
	if bpCreated {
		log.Printf("已生成访问路径，见 %s", paths.BasePath(*workDir))
	}

	handler := withRequestID(StripBasePath(auth.Wrap(mux)))
	handler = withStatusLog(withSecurityHeaders(withRecovery(withGzip(handler))))
	srv := newWebServer(handler)
	// 设置面板：改密码 / 改路径 / 改端口 / 改本地监听。
	mux.HandleFunc("/api/settings", apiSettings(auth, srv))
	updater := newUpdateManager(*workDir)
	mux.HandleFunc("/api/update/check", updater.check)
	mux.HandleFunc("/api/update/apply", updater.apply)
	mux.HandleFunc("/api/update/status", updater.status)

	log.Printf("管理界面: http://<本机IP>%s%s/", webCfg.listenAddrString(), currentBasePath())
	if base, fixed, err := fixedSlotPort(1); fixed && err == nil {
		log.Printf("Exit Slot 固定端口基址为 %d（slot 1 使用 127.0.0.1:%d）", base, base)
	} else {
		log.Printf("SOCKS5 端口在 %d-%d 之间随机分配", randPortMin, randPortMax)
	}
	if err := srv.serve(); err != nil {
		log.Print(err)
		return 1
	}
	return 0
}
