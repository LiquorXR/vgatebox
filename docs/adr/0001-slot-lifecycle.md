# ADR-0001：Slot 生命周期（目标数量 → 对账收敛）

- Status: accepted
- Date: 2026-11-06
- 范围：`vps/fanout/console.go`（applyTarget/reconcile/deleteSlot/watch）、`console_defaults.go`、`bootstrap.go`

## Context

出口（Slot）会在进程崩溃、手动删除、候选变化、重启恢复等情况下偏离用户意图。
需要的语义：用户只表达“要几个、什么策略”，后台把现实收敛到目标；崩溃重启后能从已落盘的意图继续，
而不是从运行态反推；删除必须是显式的，且删完不被后台立刻重建。

## Decision

1. 意图单点：`ExitPlan{Count, Policy, ServerName, RemoveSlot}` 落盘 `console.json`，
   所有变更先 `savePlan` 再执行（`applyTarget`、`deleteSlot`、`reconcile` 内的 RemoveSlot 清零）。
2. 对账收敛（`reconcile`，持有 `targetMu`）：先处理中断的删除（`RemoveSlot>0` 则删对应 Slot 后清零落盘），
   再从高槽位号往下缩容到 `Count`，再按策略补足到 `Count`（无候选则以占位 Node 保持 allocated/waiting，
   不自动回落直出），最后 `ensure` 内接入并清 `planError`。
3. 显式删除（`deleteSlot`）：有 plan 时 `Count-1` + 标记 `RemoveSlot` 再对账，后台不再重建；
   无 plan 的单 legacy Slot 删除转为 `Count:0 + RemoveSlot` 的显式 plan，防止下次刷新被默认初始化复活。
4. 后台 tick（`watch`）：15 秒周期 + 候选变化唤醒；`plan == nil` 且无隧道、native 模式、有候选时，
   一次性初始化默认 6 个出口（`initializeDefaultTarget`，`defaultExitTarget = 6`），已有意图（含 0）一律保留。
5. 首装确定性（`startConfiguredSlot`）：仅当环境变量给出 region/host 且无隧道时建 slot 1，
   默认关闭，不开新的公网 API。

## Consequences

- 好处：崩溃恢复 = 读盘重放意图；删除语义稳定；`planError`（remove_failed/allocation_failed/
  auto_binding_failed/no_volunteer_candidates 等）给界面明确的等待原因。
- 代价：目标与现实之间永远有收敛延迟（≤15 秒）；`Count` 语义是“想要几个”，`allocated/ready/waiting`
  由 `/api/console` 分开呈现，调用方不能拿候选数当可用数。

## Invariants（改动本机制必须保持）

- 先落盘意图、再改运行态；`RemoveSlot` 只能由内部对账写入，公开 target 接口每次清零它。
- 不自动回落直出；`unknown` 类型不视为住宅；已有数量（含 0）和旧出口不擅改。
- `console.json` 写路径永远走 `consoleJournal` 事务（见 ADR-0003）。
- 回归：`TestAPIContract`（路由）、console delete/default/refresh 相关单测。
