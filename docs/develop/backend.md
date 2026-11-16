# 后端

Go 管理进程位于 `vps/fanout/`，负责出口、隧道、内置 Xray、HTTP API、订阅、检测、更新和系统集成。这一页给改后端代码的人看；产品行为见 [产品与概念](../start/concepts.md)。

## 包与文件

```text
vps/fanout/
  main.go                 进程入口
  boot.go                 启动阶段、HTTP 装配、信号处理
  manager.go              Manager 结构与构造
  manager_lifecycle.go    出口启动/停止/换节点/删除
  manager_selectors.go    候选/隧道只读选择器
  console.go              Console 结构、出口计划、版本号
  console_api.go          /api/console、target、refresh、qr、version
  console_defaults.go     默认目标 6、初始化
  console_automation.go   周期刷新/检测
  console_check*.go       检测编排、设置、结果存储
  console_ttl.go          结果/观察/IP 类型 TTL
  console_sstp*.go        SSTP 管理/API
  console_access.go       本机网络、公网 SOCKS5
  console_native.go       自动入口维护
  console_sources.go      来源设置
  console_third_party.go  第三方代理 API
  state.go                出口状态持久化/恢复
  tunnel*.go              隧道、SOCKS5、netns 出站
  netns*.go               network namespace、DNS、守护
  killswitch.go           kill switch
  native*.go              内置 Xray 配置、入站、客户端
  sub*.go                 订阅/Clash
  policy.go               出口策略
  health.go               健康检查
  egress.go               出口画像/证据
  ip_type_cache.go        IP 类型缓存
  capacity.go             容量预检
  connect_budget.go       建连并发预算
  auth.go                 登录/会话
  settings.go             管理设置
  public_url.go           外部 origin/可信代理
  web.go                  内嵌前端/静态资源
  routes.go               路由契约
  update*.go              在线更新
  internal/paths/         路径唯一出口
  internal/store/         Store 接口 + JSON 后端
  internal/storesqlite/   SQLite 后端
```

## 核心类型

### Manager

```go
type Manager struct {
    mu        sync.RWMutex
    tunnels   map[int]*Tunnel
    nodes     []Node
    maxSlots  int
    ...
}
```

职责：

- 维护 Slot 与候选池；
- 分配端口/网段；
- 启动、停止、换节点、删除隧道；
- 健康检查与重连；
- 检测预算与诊断租约；
- 状态落盘/恢复。

主要入口：

- `Start`/`StartWithPolicy`：创建出口；
- `Stop`/`Delete`/`Resume`：生命周期；
- `Swap`/`SwapTo`：换节点；
- `RefreshNodes`：重新读取候选池；
- `ExitsOf`：把隧道和入站 join 成界面视图；
- `WatchHealth`/`WatchNodePool`：后台 worker。

### Tunnel

```go
type Tunnel struct {
    Slot   int
    Port   int
    Node   Node
    Status string
    Cred   SocksCred
    ...
}
```

关键字段/锁：

- `mu`：状态、节点、凭据、策略、延迟、客户端集合；
- `opMu`：建连/换节点/停止等长操作；
- `changeMu`：换节点/策略变更串行；
- `workCtx`/`cancel`：隧道生命周期；
- `listener`：`127.0.0.1:<port>` SOCKS5；
- `egress`：出口画像证据；
- `pendingSwap`/`pendingRotation`：换节点请求。

状态写入口：

- `setPhase`/`setPhaseLocked`：类型化状态转换，非法边拒绝并记录 `slot.phase.rejected`；
- 调用方不要直接写 `t.Status`。

### Console

职责：

- `ExitPlan` 的读写与对账；
- 检测任务/结果/TTL；
- 周期刷新与自动检测；
- 来源设置；
- SSTP/第三方代理管理；
- 控制台版本号 `version`。

`bumpConsoleVersion` 要在任何 `/api/console` 可见变更点调用；前端靠版本号决定是否拉全量。

### Native

职责：

- 加载/保存 `native.json`；
- 生成 Xray 配置并重启/重载；
- 入站 CRUD、客户端、绑定、克隆；
- 分享链接；
- REALITY/VLESS Encryption；
- 回滚 `native.previous.json`。

配置是纯函数产物：每次变更整份重建，避免半改状态。`native.unavailable` 时保持阻断，不静默直出。

## 关键流程

### 启动一条出口

```text
Manager.Start/StartWithPolicy
  → 分配 Slot/端口/网段
  → 创建 Tunnel、凭据、策略
  → saveState
  → bringUp
       ├─ setupNetns（veth、路由、DNS、kill switch）
       ├─ 写 OpenVPN 配置/凭据
       ├─ 启动 OpenVPN，等待 tun0/路由
       ├─ 建立本地 SOCKS5
       ├─ 测量出口 IP + 画像
       ├─ 校验策略/证据
       ├─ publishUp
       └─ 绑定/创建入站
```

失败时进入 `failed`/`blocked`/`waiting_for_nodes`；不会把未验证的出口发布为 `up`。

### 换节点

`Swap`/`SwapTo` 走 `rotationRequest` → `dispatchRotation` → `rotationWorker`：

- `publishUp` 只登记 `pendingRotation`；
- 真正的 `Swap`/`SwapTo`/`reconnectTo` 由单 worker 串行执行；
- 旧节点记入 `prevHost`，成功后重绑入站；
- 端口和客户端凭据不变。

### 健康检查

`WatchHealth` 每 10 秒一轮：

- 只检查 `up`/`degraded`；
- 连续 2 次失败 → 换节点；
- 延迟持续 6 轮超限 → 换节点；
- 证据失效 → 立即 `blocked` 并重连；
- 探测占用超时 → `deferred`，不改变状态。

### 检测

`startCheck` → `runChecks` → `dispatchCheckRun`：

- 候选检测使用临时诊断 Slot，不占用户容量；
- 检测和正式建连共享 `connectBudget`，正式恢复优先；
- 结果带 `profile_sha256`、`checked_at`、`expires_at`；
- 结果写 SQLite（失败回退 `checks.json`）；
- 自动检测和手动检测互斥；手动刷新会取消检测。

### 订阅

`handleSub`：

1. 校验 token（恒定时间比较）；
2. 解析 `target`/`slots`/`proxies`/`direct`/`vpn`/`third`/`bound`/`host`；
3. 收集本机网络、第三方、VPN Gate 入站；
4. Base64 或 Clash 渲染；
5. 空列表返回 404。

### 配置事务

`internal/config_transaction.go`：

- `beginConfigTransaction` 写 journal；
- 原子写目标文件；
- `commit` 删除 journal；
- 启动/写入前 `recoverConfigTransaction` 恢复未完成事务。

管理设置、`state.json`、`console.json`、`native.json` 各自有 journal。

## 不变量

改后端时必须保持：

- 先落盘意图，再改运行态；删除必须显式，后台不得立刻重建。
- 不自动回落直出；出口故障只阻断。
- `unknown` 不是住宅；手填字段不进入证据判定。
- 证据过期/配置变化必须降级，不能沿用旧结论。
- `publishUp` 只能在出口 IP/画像/绑定都验证后进入 `up`。
- `stopped` 不能由后台隐式改回 `up`；只能显式 Start/Resume。
- 换节点请求只入队，由单 worker 执行；`dispatchRotation` 在释放 `opMu` 后调用。
- 意图类文件 fail-closed；检测/画像缓存 fail-open。
- 所有路径通过 `internal/paths`，不要自己拼固定文件名。
- 路由变更必须同步 `routes.go` 和 `testdata/api/routes.json`，过 `TestAPIContract`。

## 添加一个 API

1. 在 `routes.go` 的 `allAPIRoutes()` 增加契约。
2. 在 `boot.go` 或对应 manager 的 `routes()` 注册 handler。
3. 更新 `testdata/api/routes.json`（金样本）。
4. 加 handler 测试；需要时加 `api_payload_contract_test.go`。
5. 更新 [HTTP API](../reference/api.md) 和前端 `queries.ts`/`types.ts`。
6. 跑 `TestAPIContract`、`go test ./...`、`go vet`。

匿名 `mux.HandleFunc` 会被契约测试拒绝；必须用可归因的构造器。

## 添加一个持久化字段

- 意图类：加到 `persistedTunnel`/`ExitPlan`/`nativeInbound` 等结构，设 `omitempty` 保持旧文件兼容；必要时提升 `persistedSchemaVersion`。
- 证据类：加到 `CheckResult`/`EgressProfile`，明确 TTL/过期语义。
- 不要手写路径；用 `paths` 包。
- 更新备份/恢复和迁移测试。
- 读旧文件必须能正常归一化；读不懂的新版本 fail-closed。

## 代码风格

- 错误文案以中文为主，便于界面直接展示；不要泄露 token/密码/URL。
- 不静默回退：失败要么阻断并记录原因，要么返回明确错误。
- 后台 worker 必须响应 `ctx.Done()`；停止/删除要清理 netns、veth、iptables、进程。
- 长操作不要持锁做 I/O；先取快照，释放锁后再执行。
- 新增日志用结构化字段（`slot`、`from`、`to`、`cause`），避免拼接敏感信息。
- 复杂逻辑加注释解释“为什么”，不是“做了什么”。

## 测试入口

```sh
cd vps/fanout
GOOS=linux go vet ./...
GOOS=linux go test -c -o /tmp/check .
go test ./...                 # Linux
go test -race ./...           # 夜间/CI
go test -run TestAPIContract .
```

Windows 本地不能跑完整 Go 测试（Linux-only 的 netns/pidfd/权限代码）；用 `GOOS=linux` 交叉编译 + WSL2/CI。详见 [测试](testing.md)。

## 相关

- [架构](architecture.md)
- [前端](frontend.md)
- [安装器](installer.md)
- [测试](testing.md)
- [HTTP API](../reference/api.md)
- [ADR](../adr/README.md)
