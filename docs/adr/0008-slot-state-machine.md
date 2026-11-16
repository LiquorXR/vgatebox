# ADR-0008：Slot 状态类型、转换表与换节点分派（B1/B2）

- Status: accepted（B1 + B2a 分派器 + B2b 单 worker 队列）
- Date: 2026-11-16
- 范围：`vps/fanout/slot_phase.go`、`rotation.go`、`slot_work.go`、
  `manager_lifecycle.go`、`boot.go`、`health.go`、`state.go`、`tunnel.go`、
  `prewarm.go`、`pending_test.go`、`slot_phase_test.go`

## Context

`Tunnel.Status` 是裸字符串，生产代码里 14 处直接赋值，另有大量读取
`== "up"` / `== "stopped"`。状态与 `pendingSwap`、`workCtx`、`opMu` 交织，
换节点/重连形成 8 节点调用环，改动时很难判断某个状态转换是否合法。

## Decision

1. 引入 `slotPhase` 类型与七个常量，常量值与 `state.json` 中的旧字符串
   逐字一致：`starting/up/degraded/blocked/waiting_for_nodes/failed/stopped`。
   **不新增持久化字段，不改变 schema。**
2. 引入 `slotPhaseTransitions` 转换表；`stopped` 只能由显式 Start/Resume
   离开，`up` 只能由 `publishUp` 进入，`degraded` 只允许 `up/degraded` 之间流转。
3. 所有 `Tunnel.Status` 写入改为 `setPhaseLocked`（持锁）或 `setPhase`（内部加锁）。
4. B1 阶段转换表是**观测性**的：非法边记录 `slot.phase.unexpected` warning，
   但仍按旧行为写入，避免在缺少 Linux 集成测试的改动中改变运行语义。
   转换表硬失败留作后续，需要先用全量集成测试覆盖恢复、策略与诊断路径。
5. B2 把换节点请求收敛到 `rotationRequest` + `dispatchRotation` +
   `rotationWorker`：
   - `publishUp` 只把 `pendingSwap` 转成 `pendingRotation`，不再直接
     `go swapPending`；
   - `tryCandidatesContext` 在释放 `opMu` 之后统一 `takePendingRotation` 并
     `dispatchRotation`；
   - `rotateIfPerfWorse` 也走 `dispatchRotation`（`rotationPerformance`）；
   - `dispatchRotation` 只入队；`rotationWorker` 串行执行
     `executeRotation`，`rotationAuto` → `Swap`，`rotationTarget` → `SwapTo`，
     `rotationPerformance` → 原有的 `rememberSwap + reconnectTo` 语义；
   - worker 由 `initControl` 在状态恢复前启动。不放进 `NewManager`，避免
     诊断探测的临时 Manager 每次泄漏一个常驻 goroutine。
   换节点调用环已从图谱消失（cycles 4 → 3，剩余为清理互锁、自动化重试
   与 YAML 辅助函数），这是 B2 的验收点之一。

## Consequences

- 好处：状态字符串集中定义；转换意图可读；新增日志字段 `slot/from/to`
  便于排查“为什么这条隧道变成 blocked/waiting”；换节点入口唯一，图谱中的
  换节点调用环已消失。
- 代价：仍有大量状态读取使用字符串比较；B1 不强制拒绝非法边；换节点请求
  现在经过一个全局 worker，某个 Slot 的 `changeMu` 等待会短暂推迟其他
  Slot 的换节点（每个执行体本身很快，`reconnectTo` 会另起建链 goroutine）。

## Invariants（B2 必须保持）

- `stopped` 不能由后台隐式改回 `up`；恢复流程只允许 `starting -> stopped`。
- `publishUp` 只能在 `egress != nil`、`workCtx` 与调用方一致、无重复 ExitIP 时
  把 `starting -> up`。
- `Stop/Delete` 必须取消 `workCtx`，并允许从任意非 stopped 状态进入 `stopped`；
  同时清空 `pendingSwap` 与 `pendingRotation`。
- `dispatchRotation` 只能在释放 `opMu` 后调用；它只入队，不得在内部直接
  调用 `Swap`/`SwapTo`/`reconnectTo`。`rotationWorker` 由 `initControl`
  在 `restoreState` 之前启动，否则请求会滞留在队列里。
- 执行前必须重新检查 `tunnelActive`/`stopped`，避免对已停止或已删除的
  Slot 操作；`rotationPerformance` 仍需持 `changeMu`。
- `state.json` 的 `status` 字符串与 API 字段不变；旧状态文件可直接读取。
- 回归：`slot_phase_test.go` 覆盖允许/拒绝边、空状态初始化与锁路径；
  `pending_test.go` 覆盖排队、`takePendingRotation`、worker 投递与 Stop 清理。
