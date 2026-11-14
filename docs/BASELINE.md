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

## 2. staticcheck 基线（v0.8.2，`all` 默认集）

37 条，4 个码，17 个文件。CI 门禁为默认集减这 3 个豁免（U1000 已清零，见下）：

| 码 | 数量 | 说明 |
|---|---|---|
| U1000 unused | 0 | 2026-11-09 清理：删除 15 个生产死符号 + 收编旧实现，已从 lint.yml 去掉豁免 |
| ST1005 error 首字母大写 | 21 | 中文文案风格，延后统改 |
| ST1013 数字状态码 | 1 | console_sstp.go:445（409） |
| SA2001 空临界区 | 1 | console_lifecycle.go:23（疑似用互斥做屏障，需人工复核） |

已消除：`update.go` 的 4 个 unused（`checkUpdate` 死函数，live 路径为 `UpdateManager.check`；`sha256File` 无调用者；`sha256FromList`/`extractBinary` 旧单二进制更新路径，本次连同单测一起删除，现走完整包更新）；
`main.go` 的 SA4023（`srv.serve()` 只在启动失败时返回，调用处改为直接 `log.Print`，不再判 nil）。

按文件：manager.go 10、slot_work.go 6、native_xray.go 3、state.go 3、native.go 2、tunnel.go 2、
connect_observations.go / console_lifecycle.go /
console_sstp.go / egress.go / egress_probe.go / inbound_types.go / instance_test.go /
provision.go / settings.go / socks5.go / vpngate.go 各 1。

完整行见 P0-3 采集（`staticcheck ./...` 全量输出）；消除任一码前先在 `lint.yml` 去掉对应豁免让 CI 变红，再改代码。

复现：`GOOS=linux staticcheck ./...`（vps/fanout 下，需 `go install honnef.co/go/tools/cmd/staticcheck@v0.8.2`）。

## 3. govulncheck 基线（v1.9.0，Go 1.26.0 工具链，2026-11-08 重采）

- 代码可达（Symbol）：28 个，**全部** `stdlib@go1.26.0`，均为工具链债务（fix 多在 go1.26.x 补丁版，
  如 `GO-2026-4599/4600/4601/4602 → go1.26.1`、`GO-2026-6617 → go1.26.9`、`GO-2026-6809 → go1.26.10`；
  修法是跟进 1.26.x 补丁，无需改业务代码）：
  net/http ×10（GO-2026-6617/6613/6612/6611/6610/6605/6603/6089/5026/4918）、
  crypto/x509 ×7（GO-2026-6809/5037/4947/4946/4866/4600/4599）、
  crypto/tls ×4（GO-2026-6607/6090/5856/4870）、
  net/textproto ×2（GO-2026-6608/5039）、net/url ×2（GO-2026-6218/4601）、
  encoding/asn1 ×1（GO-2026-5972）、net ×1（GO-2026-4971）、os ×1（GO-2026-4602）。
  （原 1.24.4 下 38 个；升级到 1.26.0 后旧 GO-2025-* 及部分项已消失，新增 x509 的 4866/4600/4599。）
- 包/模块级（未达代码）：Package 8 个 + Module 11 个，共 19 个 OSV，全部 stdlib、无代码调用：
  GO-2026-6609/6604/6600/6599/6091/6088/5942/5038/4986/4982/4981/4980/4977/4976/4970/4869/4865/4864/4603。
  原唯一的第三方条目 **GO-2026-5024**（`golang.org/x/sys` windows-only，已随 x/sys v0.49.0 消除）
  确认不再出现，门禁 allowlist 已清空（无条目，有即红灯）。
- CI 门禁（`scripts/govulncheck_gate.py`）：606 个 stdlib-trace findings 仅统计 → PASS；
  合成第三方 findings 反测 → FAIL。第三方模块（go-qrcode、x/sys）当前无可达漏洞。

复现：`GOTOOLCHAIN=go1.26.0 GOOS=linux govulncheck ./...`
（`go install golang.org/x/vuln/cmd/govulncheck@v1.9.0`）。

## 4. npm audit 基线（frontend）

生产依赖（`npm audit --omit=dev --audit-level=moderate`）：**0 漏洞**，CI 门禁即此命令。

dev 依赖 8 条（测试工具链，不打包进 `webdist`，修要 breaking 升级，延后）：

| 包 | 级别 |  Advisory | 修复（均为 breaking） |
|---|---|---|---|
| tinypool ≤2.1.1 | critical | GHSA-5gmw-xhrv-c9v3、GHSA-85c8-ppgw-ccpr（RCE） | vitest → 5.1.0 |
| vitest 0.0.95–4.1.10 | critical | 经由 @vitest/mocker、tinypool | vitest → 5.1.0 |
| @vitest/mocker ≤4.1.11 | moderate | GHSA-82fw-gwwq-j7x9、GHSA-qxg6-89mg-rqv8 | vitest → 5.1.0 |
| glob 9.0.0–14.0.1 | moderate | GHSA-3g3m-2fjr-6pw4 | @vue/test-utils → 2.4.11（降级，谨慎） |
| minimatch 7.0.0–11.0.0 | moderate | GHSA-g3hv-75wg-m249 | 同上 |
| js-beautify ≥1.14.9 | moderate | 经由 editorconfig、glob | 同上 |
| editorconfig ≥1.0.3 | moderate | 经由 minimatch | 同上 |
| @vue/test-utils ≥2.5.0 | moderate | 经由 js-beautify | 同上（降级） |

复现：`npm audit --json`（vps/fanout/frontend 下）。

## 5. 工具链 pin

| 工具 | CI pin | 说明 |
|---|---|---|
| Go | 1.26.0（go.mod `go 1.26.0`） | §3 的 stdlib 债务以该工具链重采，见 §6 |
| setup-go / setup-node / setup-python / checkout | 沿用现有 SHA pin | 见各 workflow |
| staticcheck | v0.8.2 | `go install honnef.co/go/tools/cmd/staticcheck@v0.8.2` |
| govulncheck | v1.9.0 | `go install golang.org/x/vuln/cmd/govulncheck@v1.9.0` |
| Node | 22（frontend engines ≥22.12） | npm audit 在此版本下采集 |

## 6. 待观察（2026-11-07 实测回填）

- `nightly-race.yml` 全量 `-race`：#2 手动跑 1分36秒通过（阈值 `-test.timeout 30m` / job 45m 富余充足）。
- `lint.yml` CI runner 实测（总数 53秒）：`go-vet 20秒 / staticcheck 50秒 / govulncheck 46秒 / npm-audit 7秒`，缓存命中正常。
- `e2e.yml` 手动跑 #24 59秒、#25 1分15秒通过；`vps.yml` 主作业 4分50秒、`test.yml` 29秒通过。
- 本地 Windows 无法执行 Go 单测（二进制含 `unix.PidfdOpen` 等 Linux-only 代码，改前即如此）；
  本地门禁 = `GOOS=linux go vet` + 交叉编译 + 各门禁脚本的本地复跑，完整执行在 Linux CI。
