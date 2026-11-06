package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
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
	loadEnvFile(envOr("FANOUT_INTELLIGENCE_ENV_FILE", "/etc/vgatebox/intelligence.env"))
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
		return runMigrationCheck(stdout, *workDir, *maxSlots, *webPort)
	}

	if os.Geteuid() != 0 {
		log.Print("需要 root 权限（要创建 netns 和改 iptables）")
		return 1
	}
	if err := os.MkdirAll(*workDir, 0700); err != nil {
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
		log.Printf("已生成访问口令，见 %s", filepath.Join(*workDir, "password"))
	}

	bpCreated, err := initBasePath(*workDir)
	if err != nil {
		log.Printf("初始化访问路径失败: %v", err)
		return 1
	}
	if bpCreated {
		log.Printf("已生成访问路径，见 %s", filepath.Join(*workDir, "basepath"))
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

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func apiNodes(m *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		nodes, fetched := m.Nodes()
		total := len(nodes)
		// 默认跟挑节点的口径一致：开了"只用家宽"就不列 VPN Gate 自营机房节点。
		// 出口画像是否住宅由 Slot 策略判断，不能拿 Residential 代替来源标签。
		// 带 all=1 能看到完整列表，用来确认过滤掉了多少。
		if residentialOnly() && r.URL.Query().Get("all") != "1" {
			kept := make([]Node, 0, len(nodes))
			for _, n := range nodes {
				if n.SourceResidential {
					kept = append(kept, n)
				}
			}
			nodes = kept
		}
		shown := len(nodes)
		writeJSON(w, http.StatusOK, map[string]any{
			"nodes":            nodes,
			"fetched":          fetched,
			"total":            total,
			"available":        shown,
			"residential_only": residentialOnly(),
		})
	}
}

func apiStop(m *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		slot, err := strconv.Atoi(r.URL.Query().Get("slot"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "slot 参数无效"})
			return
		}
		if err := m.Stop(slot); err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"ok": "已停止"})
	}
}

// apiSwap 就地把一个出口换到别的节点，端口不变。
func apiSwap(m *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		slot, err := strconv.Atoi(r.URL.Query().Get("slot"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "slot 参数无效"})
			return
		}
		if err := m.Swap(slot); err != nil {
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"ok": "正在换节点"})
	}
}

// apiRegions 是旧新建向导的兼容接口；新控制台读取 /api/console 的 regions 字段。
func apiRegions(m *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, m.Regions())
	}
}

// apiCred 改一个出口的 SOCKS5 用户名口令。两个参数都留空表示随机重置。
func apiCred(m *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Slot int    `json:"slot"`
			User string `json:"user"`
			Pass string `json:"pass"`
		}
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32768))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&in); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式无效"})
			return
		}
		if err := dec.Decode(&struct{}{}); err != io.EOF {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式无效"})
			return
		}
		if in.Slot <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "slot 参数无效"})
			return
		}
		cred, err := m.SetCred(in.Slot, SocksCred{
			User: strings.TrimSpace(in.User),
			Pass: strings.TrimSpace(in.Pass),
		})
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{
			"user": cred.User,
			"pass": cred.Pass,
		})
	}
}

// apiSettings 管理界面自身的设置：改密码 / 改路径 / 改端口 / 改本地监听。
// GET 返回当前值（不含明文口令）；POST 先验证完整请求，再事务提交所有字段。
func apiSettings(auth *Auth, srv *webServer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			var in managementSettingsRequest
			dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32768))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&in); err != nil {
				writeJSON(w, 400, map[string]string{"error": "请求格式错误"})
				return
			}
			if err := dec.Decode(&struct{}{}); err != io.EOF {
				writeJSON(w, 400, map[string]string{"error": "请求格式错误"})
				return
			}
			newPassword := ""
			if in.Password != nil {
				newPassword = strings.TrimSpace(*in.Password)
			}
			if newPassword != "" {
				if in.CurrentPassword == nil || strings.TrimSpace(*in.CurrentPassword) == "" {
					writeJSON(w, 400, map[string]string{"error": "修改密码需要输入当前密码"})
					return
				}
				if !auth.check(*in.CurrentPassword) {
					writeJSON(w, 400, map[string]string{"error": "当前密码不正确"})
					return
				}
			}
			if err := applyManagementSettings(auth, srv, in); err != nil {
				writeJSON(w, 400, map[string]string{"error": err.Error()})
				return
			}
			if newPassword != "" {
				keep := ""
				if c, err := r.Cookie(sessionCookie); err == nil {
					keep = c.Value
				}
				auth.invalidateOtherSessions(keep)
			}
		}

		cfg := getWebSettings()
		listen := cfg.ListenAddr
		if listen == "" {
			listen = "0.0.0.0"
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"base_path":        currentBasePath(),
			"port":             cfg.Port,
			"listen_addr":      listen,
			"has_password":     true,
			"residential_only": cfg.residentialOnly(),
			"version":          version,
		})
	}
}

// apiExits 返回主界面需要的一切：出口以及挂在它上面的入站。
func apiExits(m *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, m.ExitsOf())
	}
}

// apiProvision 接收"开 N 个某地区的出口"这个意图，返回作业 id 供轮询。
func apiProvision(m *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		count, err := strconv.Atoi(q.Get("count"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "count 参数无效"})
			return
		}
		tpl := 0
		if s := q.Get("template"); s != "" {
			if tpl, err = strconv.Atoi(s); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "template 参数无效"})
				return
			}
		}
		var policy *SlotPolicy
		if r.ContentLength != 0 {
			p, err := decodeSlotPolicy(w, r)
			if err != nil {
				writeJSON(w, 400, map[string]string{"error": "无效策略 JSON"})
				return
			}
			policy = &p
		}
		job, err := m.Provision(ProvisionRequest{
			Region: q.Get("region"), Count: count, TemplateID: tpl,
			EveryRegion: q.Get("every") == "1",
			Policy:      policy,
		})
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"job": job.ID()})
	}
}

func apiJobs(m *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, m.jobs.Views())
	}
}

func apiJobDismiss(m *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		m.jobs.Dismiss(r.URL.Query().Get("id"))
		writeJSON(w, http.StatusOK, map[string]string{"ok": "已关闭"})
	}
}

// apiXUIInbounds 列出内置 Xray 中已有的入站及其绑定状态。
func apiXUIInbounds(m *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		list, err := cachedInbounds(liveHosts(m))
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, list)
	}
}

// liveHosts 返回当前有连通隧道的节点标识集合。
func liveHosts(m *Manager) map[string]bool {
	live := map[string]bool{}
	for _, t := range m.Tunnels() {
		v := t.snapshot()
		if v.Status == "up" {
			live[sanitizeTag(v.Node.HostName)] = true
			live[slotOutboundTag(v.Slot)] = true
		}
	}
	return live
}

// apiXUIBind 把某个入站绑定到某条隧道，slot=0 表示解绑。
func apiXUIBind(m *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tag := r.URL.Query().Get("tag")
		if tag == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "缺少 tag 参数"})
			return
		}
		host := r.URL.Query().Get("host")
		x, err := openPanel()
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		if raw := r.URL.Query().Get("slot"); raw != "" {
			slot, parseErr := strconv.Atoi(raw)
			n, native := x.(*Native)
			if parseErr != nil || slot < 0 || !native {
				writeJSON(w, 400, map[string]string{"error": "出口编号无效"})
				return
			}
			err = n.BindSlot(tag, slot, m.Tunnels())
		} else {
			err = x.Bind(tag, host, m.Tunnels())
		}
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		invalidateInbounds()
		writeJSON(w, http.StatusOK, map[string]string{"ok": "已更新"})
	}
}

// apiXUIClone 以某个入站为模板，为所有已连通的隧道各复制一个入站并绑好出口。
func apiXUIClone(m *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.Atoi(r.URL.Query().Get("id"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id 参数无效"})
			return
		}

		tunnels := m.Tunnels()
		// 用节点主机名而非槽位号：槽位在重启后会重排，指代会错位
		var hosts []string
		if raw := r.URL.Query().Get("hosts"); raw != "" {
			for _, part := range strings.Split(raw, ",") {
				if h := strings.TrimSpace(part); h != "" {
					hosts = append(hosts, h)
				}
			}
		} else {
			for _, t := range tunnels {
				if t.Status == "up" {
					hosts = append(hosts, t.Node.HostName)
				}
			}
		}
		if len(hosts) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "没有可用的隧道"})
			return
		}

		x, err := openPanel()
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		ports, err := x.CloneToTunnels(id, hosts, tunnels)
		invalidateInbounds()
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error(), "created": ports})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"created": ports})
	}
}

// apiXUIDetail 返回某个入站的详情，含客户端与可直接复制的分享链接。
func apiXUIDetail(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.URL.Query().Get("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id 参数无效"})
		return
	}
	x, err := openPanel()
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	host := r.URL.Query().Get("host")
	if host == "" {
		host = publicHost(r)
	}
	detail, err := x.InboundDetail(id, host)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

// publicHost 决定分享链接里的连接地址。母机公网 IPv4 才是客户端真正能连上
// 的地址，所以优先用它；探测不到（比如纯内网）再退回访问 fanout 时用的主机名。
func publicHost(r *http.Request) string {
	if ip := hostPublicIP(); ip != "" {
		return ip
	}
	host := publicHostWithoutPort(r)
	if host == "" || host == "127.0.0.1" || host == "localhost" {
		return "<服务器IP>"
	}
	return host
}

// apiXUILinks 批量导出多个入站的分享链接。
func apiXUILinks(w http.ResponseWriter, r *http.Request) {
	x, err := openPanel()
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}

	var ids []int
	if raw := r.URL.Query().Get("ids"); raw != "" {
		for _, part := range strings.Split(raw, ",") {
			if n, err := strconv.Atoi(strings.TrimSpace(part)); err == nil {
				ids = append(ids, n)
			}
		}
	} else {
		list, err := x.Inbounds(nil)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		for _, ib := range list {
			ids = append(ids, ib.ID)
		}
	}
	if len(ids) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "没有可导出的入站"})
		return
	}

	host := r.URL.Query().Get("host")
	if host == "" {
		host = publicHost(r)
	}
	links, err := x.InboundLinks(ids, host)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error(), "links": links})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"links": links})
}

// apiXUIRotateEncryption 重新生成一个 VLESS 入站的协议内加密参数。
func apiXUIRotateEncryption(m *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.Atoi(r.URL.Query().Get("id"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id 参数无效"})
			return
		}
		p, err := openPanel()
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		n, ok := p.(*Native)
		if !ok {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "仅支持内置 Xray"})
			return
		}
		enc, err := n.RotateVLESSEncryption(id, m.Tunnels())
		invalidateInbounds()
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{
			"client_encryption": enc.ClientEncryption,
			"key_type":          enc.KeyType,
		})
	}
}

// apiXUISetEncryption 打开或关闭一个 VLESS 入站的协议内加密层。
func apiXUISetEncryption(m *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.Atoi(r.URL.Query().Get("id"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id 参数无效"})
			return
		}
		enabled := r.URL.Query().Get("enabled") == "1"
		keyType := r.URL.Query().Get("key_type")
		p, err := openPanel()
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		n, ok := p.(*Native)
		if !ok {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "仅支持内置 Xray"})
			return
		}
		enc, err := n.SetVLESSEncryption(id, enabled, keyType, m.Tunnels())
		invalidateInbounds()
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if !enabled || enc == nil {
			writeJSON(w, http.StatusOK, map[string]bool{"enabled": false})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"enabled":           true,
			"client_encryption": enc.ClientEncryption,
			"key_type":          enc.KeyType,
		})
	}
}

// apiXUIDelete 删除入站。停掉出口后它的入站会留下来，用户需要一个清理入口。
func apiXUIDelete(m *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var ids []int
		for _, part := range strings.Split(r.URL.Query().Get("ids"), ",") {
			if n, err := strconv.Atoi(strings.TrimSpace(part)); err == nil {
				ids = append(ids, n)
			}
		}
		if len(ids) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "没有指定要删除的入站"})
			return
		}
		x, err := openPanel()
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		err = x.DeleteInbounds(ids, m.Tunnels())
		invalidateInbounds()
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]int{"deleted": len(ids)})
	}
}

// apiInboundUpdate 修改内置 Xray 入站的端口、备注与启停。
func apiInboundUpdate(m *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, err := openPanel()
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		q := r.URL.Query()
		id, err := strconv.Atoi(q.Get("id"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id 参数无效"})
			return
		}

		// 只有真正传了的参数才改，没传的保持原样
		var patch InboundPatch
		if mode := q.Get("name_mode"); mode != "" {
			patch.NameMode = &mode
		}
		if v := q.Get("port"); v != "" {
			port, err := strconv.Atoi(v)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "端口无效"})
				return
			}
			patch.Port = &port
		}
		if q.Has("remark") {
			remark := q.Get("remark")
			patch.Remark = &remark
		}
		if v := q.Get("enable"); v != "" {
			enable := v == "1"
			patch.Enable = &enable
		}

		err = p.UpdateInbound(id, patch, m.Tunnels())
		invalidateInbounds()
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"ok": "已保存"})
	}
}

// clientAction 把三个客户端操作的公共部分收拢：解析 id/email 再调后端。
func clientAction(m *Manager, what string,
	do func(p Panel, id int, email string, tunnels []*Tunnel) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, err := openPanel()
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		id, err := strconv.Atoi(r.URL.Query().Get("id"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id 参数无效"})
			return
		}
		err = do(p, id, r.URL.Query().Get("email"), m.Tunnels())
		invalidateInbounds()
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"ok": what})
	}
}

func apiClientAdd(m *Manager) http.HandlerFunc {
	return clientAction(m, "已添加", func(p Panel, id int, email string, t []*Tunnel) error {
		return p.AddClient(id, email, t)
	})
}

func apiClientDelete(m *Manager) http.HandlerFunc {
	return clientAction(m, "已删除", func(p Panel, id int, email string, t []*Tunnel) error {
		return p.DeleteClient(id, email, t)
	})
}

func apiClientReset(m *Manager) http.HandlerFunc {
	return clientAction(m, "已重置", func(p Panel, id int, email string, t []*Tunnel) error {
		return p.ResetClient(id, email, t)
	})
}

func apiInboundCreate(m *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, err := openPanel()
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}

		q := r.URL.Query()
		port, _ := strconv.Atoi(q.Get("port"))
		kcpMTU, _ := strconv.Atoi(q.Get("kcp_mtu"))
		kcpTTI, _ := strconv.Atoi(q.Get("kcp_tti"))
		kcpUplink, _ := strconv.Atoi(q.Get("kcp_uplink"))
		kcpDownlink, _ := strconv.Atoi(q.Get("kcp_downlink"))
		slot := 0
		if raw := q.Get("slot"); raw != "" {
			var parseErr error
			slot, parseErr = strconv.Atoi(raw)
			_, native := p.(*Native)
			if parseErr != nil || slot < 0 || !native {
				writeJSON(w, 400, map[string]string{"error": "出口编号无效"})
				return
			}
		}
		ib, err := p.CreateInbound(NewInboundSpec{
			BoundSlot: slot,
			Protocol:  q.Get("protocol"),
			Network:   q.Get("network"),
			Port:      port,
			Remark:    q.Get("remark"),
			Path:      q.Get("path"),
			Host:      q.Get("host"),
			Security:  q.Get("security"),
			Vision:    q.Get("vision") == "1",

			ServerName: q.Get("sni"),
			CertFile:   q.Get("cert"),
			KeyFile:    q.Get("key"),

			Dest:        q.Get("dest"),
			ServerNames: q.Get("server_names"),
			ShortID:     q.Get("sid"),
			Fingerprint: q.Get("fp"),

			KCPMTU:      kcpMTU,
			KCPTTI:      kcpTTI,
			KCPUplink:   kcpUplink,
			KCPDownlink: kcpDownlink,

			VLESSEncryption:     q.Get("vless_encryption") == "1",
			VLESSEncryptionType: q.Get("vless_encryption_type"),
		}, m.Tunnels())
		invalidateInbounds()
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"id":       ib.ID,
			"port":     ib.Port,
			"protocol": ib.Protocol,
			"remark":   ib.Remark,
			"network":  ib.Network,
			"security": ib.Security,
		})
	}
}
