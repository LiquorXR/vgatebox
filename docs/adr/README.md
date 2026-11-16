# 设计决策记录（ADR）

这些是 vgatebox 在设计/重构过程中做出的决策记录，用于解释“为什么这样设计”和“改动时必须保持什么”。它们是**历史记录**，不是当前用户文档；当前行为以 `docs/start/`、`docs/use/`、`docs/operate/`、`docs/reference/` 为准。

- 状态：`accepted` 表示当时接受；`superseded` 表示已被更新决策取代。
- 新决策按顺序编号，不要改旧 ADR 的结论；被取代时更新 Status 并链接新 ADR。
- 结构：Context / Decision / Consequences / Invariants。

| ADR | 主题 | 状态 | 日期 |
| --- | --- | --- | --- |
| [0001](0001-slot-lifecycle.md) | Slot 生命周期：目标数量 → 对账收敛 | accepted | 2026-11-06 |
| [0002](0002-evidence-chain.md) | 证据链：检测结论的构成与失效 | accepted | 2026-11-06 |
| [0003](0003-config-transactions.md) | 配置事务：先落盘意图，再改运行态 | accepted | 2026-11-06 |
| [0004](0004-subscription-format.md) | 订阅格式：v2rayN/Clash 输出契约 | accepted | 2026-11-06 |
| [0005](0005-update-transaction.md) | 更新事务：完整包、备份、恢复 | accepted | 2026-11-06 |
| [0006](0006-xray-reload.md) | Xray 重载：整份重建与阻断 | accepted | 2026-11-06 |
| [0007](0007-sqlite-scope.md) | SQLite 只收自有高频数据，意图留在 JSON | accepted | 2026-11-06 |
| [0008](0008-slot-state-machine.md) | Slot 状态类型、转换表与换节点分派 | accepted | 2026-11-16 |
| [0009](0009-boot-phases.md) | 启动阶段拆分 | accepted | 2026-11-16 |

## 与当前文档的关系

- 想理解产品概念：看 [产品与概念](../start/concepts.md)。
- 想改出口/检测/订阅/更新：先读相关 ADR 的 Invariants。
- ADR 中出现的任务编号（如 P0/C1/D6）是当时的工作项，不属于用户文档语境。
- ADR 的日期和范围保留原样；不要为了“看起来统一”改写历史结论。

## 新增 ADR

1. 复制现有结构，编号取下一个未使用的四位数。
2. 写清 Context（问题/约束）、Decision（选了什么）、Consequences（代价/收益）、Invariants（后续改动必须保持）。
3. 在 `docs/README.md` 的文档地图和本页索引加链接。
4. 如果取代旧 ADR，更新旧 ADR 的 Status 并链接新 ADR。
5. 跑 `python3 scripts/check_docs.py`。
