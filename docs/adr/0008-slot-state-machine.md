# ADR-0008：Slot 状态类型、转换表与换节点分派（B1/B2a）

- Status: accepted（B1 + B2a 分派器；单 worker 循环待后续）
- Date: 2026-11-16
- 范围：`vps/fanout/slot_phase.go`、`rotation.go`、`slot_work.go`、
  `manager_lifecycle.go`、`health.go`、`state.go`、`tunnel.go`、
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
   B2b 完成单 worker 循环后，再把非法边改成硬失败。
5. B2a 把换节点请求收敛到 `rotationRequest` + `dispatchRotation`：
   - `publishUp` 只把 `pendingSwap` 转成 `pendingRotation`，不再直接
     `go swapPending`；
   - `tryCandidatesContext` 在释放 `opMu` 之后统一 `takePendingRotation` 并
     `dispatchRotation`；
   - `rotateIfPerfWorse` 也走 `dispatchRotation`（`rotationPerformance`），
     不再直接递归 `reconnectTo`；
   - `rotationAuto` → `Swap`，`rotationTarget` → `SwapTo`，
     `rotationPerformance` → 原有的 `rememberSwap + reconnectTo` 语义。
   生产调用环从 8 节点缩为 7 节点，入口统一为 `dispatchRotation`；
   真正断开环需要 B2b 的单 worker 队列（`dispatchRotation` 只入队，不再
   直接调用 `Swap`），本次不强行引入。

## Consequences

- 好处：状态字符串集中定义；转换意图可读；新增日志字段 `slot/from/to`
  便于排查“为什么这条隧道变成 blocked/waiting”。
- 代价：仍有大量状态读取使用字符串比较；B1 不强制拒绝非法边。
  这是分阶段落地的刻意取舍，不是最终形态。

## Invariants（B2 必须保持）

- `stopped` 不能由后台隐式改回 `up`；恢复流程只允许 `starting -> stopped`。
- `publishUp` 只能在 `egress != nil`、`workCtx` 与调用方一致、无重复 ExitIP 时
  把 `starting -> up`。
- `Stop/Delete` 必须取消 `workCtx`，并允许从任意非 stopped 状态进入 `stopped`；
  同时清空 `pendingSwap` 与 `pendingRotation`。
- `dispatchRotation` 只能在释放 `opMu` 后调用；执行前必须重新检查
  `tunnelActive`/`stopped`，避免对已停止或已删除的 Slot 操作。
- `state.json` 的 `status` 字符串与 API 字段不变；旧状态文件可直接读取。
- 回归：`slot_phase_test.go` 覆盖允许/拒绝边、空状态初始化与锁路径；
  `pending_test.go` 覆盖排队、`takePendingRotation` 与 Stop 清理。
