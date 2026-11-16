# 基线数据（P0）

记录 P0 基线阶段完成时的可复核数字：入口复杂度、路由契约覆盖、lint 豁免债务。
所有数字都注明来源与复现方法；改动相关代码后按 §6 重采并更新本文件。

- 基线提交：`8e12244`（main，P0-1/P1-1、P0-2、P0-3 全部合入后）
- 采集日期：2026-11-06
- 本文件补齐 `lint.yml` 注释中引用的基线明细（P0-4）。

## 1. 入口与路由基线（图谱 + 契约）

| 指标 | 基线前（`8d44e82` 图谱） | 基线（`8e12244` 图谱） | 口径 |
|---|---|---|---|
| `main()` | 213 行 / 复杂度 39 / 认知 41（main.go:21–233） | 3 行 / 0 / 0（main.go:21–23，只剩 `os.Exit(runFanout(...))`） | 图谱 Function 节点 |
| `runFanout()` | —（不存在） | 238 行 / 41 / 44（main.go:29–266） | 逻辑原样搬运，下一步拆分目标 |
| HANDLES 归因边 | 23 | 28（+5：apiApplyTarget、apiConsoleQR、Sstp/ThirdParty 的 apiStartCheck、Sstp 的 apiCancelCheck） | 图谱 HANDLES 边 |
| Route 节点 | 62 | 62（后端 56 静态 + 前端/夹具 6；third-party delete/toggle/subscribe 为动态注册，图谱无节点） | 图谱 Route 节点 |
| API 契约 | — | 59/59（金样本 = 代码表 = 注册点，`TestAPIContract` 四项全过） | `routes.go` + `testdata/api/routes.json` |
| 匿名 HandleFunc | 7 处 | 0 处 | `TestAPIContract/NoAnonymousRouteHandlers` |

HANDLES 仍缺 31 条后端路由：全部是 `apiXxx(mgr)` 闭包构造器包装的（nodes/stop/swap/policy/rollback/lifecycle/cred/regions/provision/jobs/exits/export/xui×8/panel×5/sub×2/settings/update×3 等）
与 third-party delete/toggle/subscribe（工厂函数动态注册）——图谱只归因直接方法值。
这部分由 `TestAPIContract/HandlesCoverage` 做源码级全覆盖（59/59），与图谱互补。

复现：

```sh
# 契约（Linux CI 跑全量；Windows 本地只能交叉验证）
GOOS=linux go vet ./...                                   # vps/fanout 下
GOOS=linux go test -c -o /tmp/check .                     # 含 TestAPIContract 的编译检查
VGATEBOX_UPDATE_API_GOLDEN=1 GOOS=linux go test -run TestAPIContract .  # 金样本自重写（与 routes.go 同源）
```

### 1.1 启动拆分后（2026-11-16，分支 `refactor/boot-phases`）

| 指标 | 拆分前 | 拆分后 | 口径 |
|---|---|---|---|
| `runFanout()` | 238 行 / 复杂度 41 / 认知 44（§1 基线记录） | 51 行 / 9 / 9（main.go） | 图谱 Function 节点 |
| `bootstrapHost()` | — | 49 行 / 12 / 13（boot.go） | 图谱 Function 节点 |
| `initControl()` | — | 31 行 / 7 / 9 | 图谱 Function 节点 |
| `initHTTP()` + `registerRoutes()` | — | 32 / 4 / 4 + 40 / 0 / 0 | 图谱 Function 节点 |
| `installShutdownHandler()` + `serveUntilSignal()` | — | 13 / 1 / 1 + 4 / 0 / 0 | 图谱 Function 节点 |

启动顺序、日志文案与退出码不变；`boot_test.go` 覆盖默认值、显式 flag、未知 flag、
`-h`、容量边界、`FANOUT_PUBLIC_IP` 回退、`-version` 与 `-check-state`。
本表在 BASELINE 全量重采时并入 §1 的总表，并注明新提交号。

B1（Slot 状态类型）同步落地：`slot_phase.go` 定义 `starting/up/degraded/blocked/
waiting_for_nodes/failed/stopped` 转换表，生产代码 14 处直接 `Status` 写入收敛为
`setPhaseLocked`/`setPhase`；转换表见 ADR-0008。

B3 同步落地：转换表从 warning 改为硬约束，非法边记录 `slot.phase.rejected`
并保持原状态；`slot_phase_test.go` 覆盖 stopped→up 的拒绝路径。

B2（换节点分派器 + 单 worker 队列）同步落地：`rotation.go` 引入
`rotationRequest`/`dispatchRotation`/`rotationWorker`；`publishUp` 只登记
`pendingRotation`，`tryCandidatesContext` 退出 `opMu` 后入队，
`rotateIfPerfWorse` 也走同一入口。worker 由 `initControl` 在状态恢复前启动。
图谱换节点调用环已消失（cycles 4 → 3，剩余为清理互锁、自动化重试与
YAML 辅助函数）。

前端 C1 同步落地：`ExitsPage.vue` 从 863 行降到 387 行；目标对话框
`ExitTargetDialog.vue`（186 行）、详情抽屉 `ExitDetailDrawer.vue`（350 行）、
策略/倒计时逻辑分别在 `lib/policy.ts` 与 `lib/exit-time.ts`。Vitest 从
66 增至 73 个用例。

前端 C2 同步落地：`AccessPage.vue` 从 713 行降到 52 行；三个 tab 拆为
`SocksAccessTab.vue`（211 行）、`SstpAccessTab.vue`（205 行）、
`ThirdPartyAccessTab.vue`（302 行），错误处理收敛到 `lib/access-error.ts`。

前端 C3 同步落地：`AdvancedAccessPanel.vue` 从 702 行降到 329 行；
新增入口对话框 `InboundCreateDialog.vue`（183 行）、详情抽屉
`InboundDetailDrawer.vue`（292 行），入站命名/绑定/状态判断收敛到
`lib/inbound.ts`（46 行）。

前端 C4 同步落地：`NodesPage.vue` 从 581 行降到 400 行；筛选栏、节点行、
检测进度、侧栏、来源设置、周期任务与节点详情分别拆到 `pages/nodes/`。
至此所有页面/组件均 ≤ 400 行（`ExitsPage` 387、`AdvancedAccessPanel` 329、
`NodesPage` 400）。

### 1.2 后端复杂度热点（D，2026-11-16）

| 函数 | 复杂度（前 → 后） | 拆分 |
|---|---|---|
| `clashProxy` | 32 → 4 | `parseClashLink` / `applyClashTransport` / `applyClashSecurity` / `applyClashTLS` / `applyClashReality` / `applyClashVLESSOptions` |
| `subLinksSources` | 30 → 4 | `subLinkOptions` + 原生/直出/第三方/Slot 链接分段 |
| `commitManagementSettings` | 30 → 7 | 校验、目录守卫、监听准备、文件写入、运行时应用 |
| `checkMigrationState` | 30 → 4 | 期刊、计划、SSTP、设置、密码、basepath、state、native 分项检查 |
| `normalizeInboundSpec` | 25 → 4 | 名称/端口/路径/备注/加密/Vision/KCP 分段 |
| `shareLink` | 24 → 1 | 传输、TLS、REALITY、加密参数分段 |
| `handleSub` | 23 → 5 | 鉴权、target、订阅选项、响应写出分段 |
| `dispatchCheckRun` | 23 → 12 | 入队、取消、空闲完成、诊断槽位分段 |
| `normalizePolicy` | 21 → 6 | 国家、IP 类型、文本列表、冲突检查分段 |
| `buildXrayConfig` | 20 → 0 | 隧道索引、入站、出站、路由规则、tag 归因分段 |
| `mirror_source.fetch_mirror` | 22 → 2 | 索引解析、快照年龄、回滚、下载校验分段 |

生产函数 complexity ≥ 20 的数量从 11 降到 **0**。

### 1.3 工具链与容量（E1/F1，2026-11-16）

- Go 工具链 `1.26.0 → 1.26.11`（`go.mod` 与 6 处 workflow pin 同步）。
- `GOTOOLCHAIN=go1.26.11 GOOS=linux govulncheck ./...` → `No vulnerabilities found.`
  （旧 1.26.0 下的 28 个 stdlib symbol findings 与 19 个 package/module OSV 全部消失）。
- 新增 `capacity.go`：按 fd、`MemAvailable`、端口区间给出保守建议，启动日志输出
  “容量预检”；不改变 `-max`，不阻止创建。
- 新增 `TestFreeRandomPortExhausted` 与容量估算表驱动测试。

### 1.4 当前基线汇总（2026-11-16，代码采集自 `d5757bd`）

| 指标 | 当前值 | 口径 |
|---|---|---|
| 图谱规模 | 4,518 节点 / 21,605 边 | codebase-memory-mcp |
| Go 生产/测试行数 | 18,846 / 12,857 | git 跟踪文件 |
| Go 测试函数 | 454 | `func Test/Fuzz/Benchmark` |
| Python 测试函数 | 123 | `def test_` |
| 前端用例 | 73 | Vitest 5.1.0 |
| `runFanout` | 51 行 / 9 / 9 | 图谱 Function |
| 生产函数 complexity ≥ 20 | **0** | 图谱 Function |
| 调用环 | 3 | 清理互锁、自动化重试、YAML 互调 |
| 路由契约 | 62/62 | `TestAPIContract` 源码覆盖；图谱 HANDLES 30/62 |
| staticcheck | 21 条 ST1005 | 仅保留中文文案豁免 |
| govulncheck | 0 | Go 1.26.11 |
| npm audit | 生产 0 / dev 4 moderate | Vitest 5.1.0 + @vue/test-utils 2.4.11 |

## 2. staticcheck 基线（v0.8.2，`all` 默认集）

21 条，1 个码，7 个文件。CI 门禁为默认集减 ST1005（中文文案首字母风格，保留豁免）：

| 码 | 数量 | 说明 |
|---|---|---|
| U1000 unused | 0 | 2026-11-09 清理：删除 15 个生产死符号 + 收编旧实现，已从 lint.yml 去掉豁免 |
| ST1005 error 首字母大写 | 21 | 中文文案风格；统改会动用户可见错误文案，保留豁免 |
| ST1013 数字状态码 | 0 | 2026-11-16 修复：`console_sstp_api.go:167` 409 → `http.StatusConflict` |
| SA2001 空临界区 | 0 | 2026-11-16 修复：`console_lifecycle.go` 的互斥屏障改为显式读取 `plan` |

按文件：manager_lifecycle.go 6、slot_work.go 6、state.go 3、native_inbounds.go 2、
native_xray.go 2、provision.go 1、tunnel_socks.go 1。

复现：`GOOS=linux staticcheck ./...`（vps/fanout 下，需 `go install honnef.co/go/tools/cmd/staticcheck@v0.8.2`）。
CI 用 `staticcheck -checks '-ST1005' ./...`；除 ST1005 外新增 finding 一律红灯。

## 3. govulncheck 基线（v1.9.0，Go 1.26.11 工具链，2026-11-16 重采）

- 代码可达（Symbol）：**0**。
- 包/模块级（未达代码）：**0**。
- 旧 1.26.0 工具链下的 28 个 stdlib symbol findings 与 19 个 package/module OSV
  已随 1.26.11 补丁全部消失；本次无业务代码改动。
- 第三方模块（`go-qrcode`、`x/sys`）仍为 0 可达漏洞。
- CI 门禁（`scripts/govulncheck_gate.py`）逻辑不变：stdlib findings 只统计，
  任何第三方模块 finding 一律红灯；当前两侧均为空。

复现：`GOTOOLCHAIN=go1.26.11 GOOS=linux govulncheck ./...` → `No vulnerabilities found.`
（`go install golang.org/x/vuln/cmd/govulncheck@v1.9.0`）。

## 4. npm audit 基线（frontend）

生产依赖（`npm audit --omit=dev --audit-level=moderate`）：**0 漏洞**，CI 门禁即此命令。

dev 依赖 4 条 moderate（测试工具链，不打包进 `webdist`）：

| 包 | 级别 | Advisory | 说明 |
|---|---|---|---|
| glob 9.0.0–14.0.1 | moderate | GHSA-3g3m-2fjr-6pw4 | 经由 js-beautify → @vue/test-utils 2.4.11 |
| minimatch 7.0.0–11.0.0 | moderate | GHSA-g3hv-75wg-m249 | 同上 |
| js-beautify ≥1.14.9 | moderate | 经由 editorconfig、glob | 同上 |
| editorconfig ≥1.0.3 | moderate | 经由 minimatch | 同上 |

2026-11-16：vitest `3.2.7 → 5.1.0`，`@vue/test-utils` 降到 `2.4.11`；tinypool/vitest 的
critical RCE 链已清除，`npm audit` 从 2 critical + 6 moderate 降为 0 critical + 4 moderate。
剩余 4 条是 `@vue/test-utils` 的 dev 传递依赖，强制修复需要再次调整测试工具链；当前
生产依赖仍为 0 漏洞，CI 门禁继续使用 `npm audit --omit=dev --audit-level=moderate`。

复现：`npm audit --json`（vps/fanout/frontend 下）。

## 5. 工具链 pin

| 工具 | CI pin | 说明 |
|---|---|---|
| Go | 1.26.11（go.mod `go 1.26.11`） | §3 的 stdlib 债务已清零，见 §6 |
| setup-go / setup-node / setup-python / checkout | 沿用现有 SHA pin | 见各 workflow |
| staticcheck | v0.8.2 | `go install honnef.co/go/tools/cmd/staticcheck@v0.8.2` |
| govulncheck | v1.9.0 | `go install golang.org/x/vuln/cmd/govulncheck@v1.9.0` |
| Node | 22（frontend engines ≥22.12） | npm audit 在此版本下采集 |

## 6. 工具观察（2026-11-16 更新）

- `nightly-race.yml` 全量 `-race`：#2 手动跑 1分36秒通过（阈值 `-test.timeout 30m` / job 45m 富余充足）。
- `lint.yml` CI runner 实测（总数 53秒）：`go-vet 20秒 / staticcheck 50秒 / govulncheck 46秒 / npm-audit 7秒`，缓存命中正常。
- `e2e.yml` 手动跑 #24 59秒、#25 1分15秒通过；`vps.yml` 主作业 4分50秒、`test.yml` 29秒通过。
- 本地 Windows 无法执行 Go 单测（二进制含 `unix.PidfdOpen` 等 Linux-only 代码，改前即如此）；
  本地门禁 = `GOOS=linux go vet` + 交叉编译 + 各门禁脚本的本地复跑，完整执行在 Linux CI。
- `codebase-memory-mcp` 索引的已知缺口（2026-11-16）：`vps/deploy` 被工具 skip-list 整棵排除；
  `src/styles/main.css` 的 Tailwind `@custom-variant`（第 5 行）局部解析失败；62 条路由中
  HANDLES 仅归因 30 条（闭包构造器）。三者都不影响 `TestAPIContract` 的 62/62 源码级覆盖，
  安装器/卸载器由 `tests/test_installer.py`、`tests/test_uninstall.py` 覆盖。
- 剩余 3 个 CALLS 环（2026-11-16）：`stop/Close/blockWork` 清理互锁、`automationTick/
  waitRefreshThenTick` 重试推进、`writeYAMLMap/writeYAMLField` 纯递归写入。前三者是有意的
  控制流，最后一个无并发、无 I/O；D6 不再为了图谱指标强行拆分。
