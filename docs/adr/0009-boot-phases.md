# ADR-0009：启动阶段拆分（runFanout 只做编排）

- Status: accepted
- Date: 2026-11-16
- 范围：`vps/fanout/main.go`（入口与编排）、`vps/fanout/boot.go`（阶段实现）、
  `vps/fanout/boot_test.go`（参数与早退回归）

## Context

`runFanout` 原先同时承担 flag 解析、root 检查、工作目录与实例锁、netns 初始化、
管理配置事务恢复、面板配置、Web 设置、Manager/Console 构建、状态恢复、
后台 worker 启动、路由注册、鉴权/访问路径、中间件与信号处理。任何一处调整
都要在一个 200+ 行的函数里读完整条启动链，难以单测，也难以判断阶段顺序。

## Decision

把启动流程拆成六个阶段，`runFanout` 只按顺序调用：

1. `parseBootConfig`：只做 flag 解析与 `portExplicit` 标记，不校验业务范围、
   不触碰文件系统；`-h` 返回 0，解析失败返回 2。
2. `validateBootConfig`：出口容量校验、`FANOUT_PUBLIC_IP` 回退；保持原顺序
   “先校验容量，再补公网 IP”。
3. `bootstrapHost`：root 检查 → 工作目录 → 记录母机 netns → 实例锁 →
   实例标识 → management 事务恢复 → 公网 IP 预热 → `prepareHost` → 面板配置。
   返回 `hostRuntime`，其 `close()` 幂等地关闭面板并释放实例锁。
4. `initControl`：`NewManager` → 首次候选拉取 → `newConsole` →
   `cleanupDiagnostics` → `restoreState` → `startConfiguredSlot` →
   `OnTunnelsChanged`。
5. `startBackground`：watch/automation/checks/SSTP/health/node-pool 六个常驻 worker。
6. `initHTTP`：`registerRoutes` → `NewAuth` → `initBasePath` → 中间件 →
   `newWebServer` → settings/update 路由；`serveUntilSignal` 负责信号与阻塞服务。

`runFanout` 仍保留原签名 `func runFanout(args []string, stdout, stderr io.Writer) int`，
`main()` 仍只有 `os.Exit(runFanout(...))`，因此 `TestAPIContract/MainDelegatesToRunFanout`
无需改变。

## Consequences

- 好处：每个阶段可独立测试；启动顺序成为显式代码而不是注释；错误信息与退出码
  可以按阶段锁定；后续替换存储/面板/路由时有明确落点。
- 代价：多了一个 `boot.go` 文件与 `hostRuntime`/`controlRuntime`/`httpRuntime`
  三个轻量结构；阶段之间仍共享包级全局（如 `instTag`、`panelState`），
  本次不引入依赖注入容器。

## Invariants（改动本机制必须保持）

- 启动顺序不变：root 检查必须在任何文件写入之前；`initMainNetns` 必须在建隧道之前；
  management → console → native → state 的事务恢复顺序不变。
- 退出码不变：`-h` 0；解析失败 2；容量非法 1；启动阶段失败 1；`serve` 返回 1。
- `-version` / `-check-state` 必须在 root 检查之前返回，且不要求 root。
- `hostRuntime.close()` 必须幂等：signal 路径与 `serve` 失败可能竞争。
- 回归：`boot_test.go` 覆盖默认值、显式 flag、未知 flag、`-h`、容量边界、
  公网 IP 回退、`-version`、`-check-state`。
