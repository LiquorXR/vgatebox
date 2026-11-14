# ADR-0003：四组配置事务（journal + 原子写 + 启动恢复）

- Status: accepted
- Date: 2026-11-06
- 范围：`vps/fanout/config_transaction.go`（机制）、`management_transaction.go`、`console.go`（savePlan）、
  `native_transaction.go`、`state.go`、`main.go`（启动恢复顺序）

## Context

管理配置、出口计划、内置 Xray 配置、隧道状态是四份独立的落盘状态，任何一份写一半崩溃都会导致
“配置与运行态不一致”且无法区分。需要每组独立可恢复：崩溃后要么回到上一次提交，要么拒绝启动，
绝不带着半份配置继续跑。

## Decision

1. 统一机制（`config_transaction.go`）：journal 是有界 undo 记录（版本号 + 文件名前像，私用，
   永不经 API 返回，提交后才清除）。`begin` 在 journal 已存在时直接拒绝；写文件一律
   `atomicConfigWrite`（同目录临时文件 + 0600 + fsync + rename + 目录 sync）；
   `commit` 即删 journal；`rollback` 先重写 journal 再逐文件还原，最后提交。
2. 启动恢复（`recoverConfigTransaction`）先校验再回滚：版本必须为 1、文件集合必须与受管集合完全一致、
   路径不得逃逸（仅 basename）、`!Exists && len(Data)>0` 视为损坏——任一失败都拒绝启动，保留现场。
3. 四组划分（各走独立 journal，组间无原子性，各自回到自己的上次提交）：
   - management：`.management-transaction.json` ← settings.json / password / basepath；
   - console：`.console-transaction.json` ← console.json（出口计划）；
   - native：`.native-transaction.json` ← native.json / xray.json / native.previous.json；
   - state：`.state-transaction.json` ← state.json（隧道状态）。
4. 启动恢复顺序（`runFanout` → `newConsole` → panel → state）：先 management，再 console，
   再 native，最后 state；`native.previous.json` 保留一次可回滚的内置配置（`/api/config/rollback`）。

## Consequences

- 好处：单文件损坏只影响本组；恢复语义可测试（损坏即报错不启动）。
- 代价：跨组变更（如改端口 + 改计划）不是原子的，调用方必须接受中间态；
   journal 文件是内部实现，不得加入备份导出或对外 API。

## Invariants（改动本机制必须保持）

- 受管文件名只能是 basename，不得含路径；恢复记录的文件集合必须精确匹配。
- 提交前 journal 必须存在，提交后必须消失；`begin` 遇到残留 journal 必须拒绝而不是覆盖。
- 启动时四组按顺序恢复，失败即拒绝启动（`runMigrationCheck` 只读检查同理）。
- 回归：`config_transaction_test.go`（版本/集合/逃逸/损坏矩阵）。
