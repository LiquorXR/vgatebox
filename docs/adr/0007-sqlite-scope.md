# ADR-0007：SQLite 只收自有高频数据，意图类文件留在 JSON

- Status: accepted
- Date: 2026-11-06
- 范围：`internal/store`（接口 + JSON 后端）、`internal/storesqlite`
  （modernc 纯 Go 后端）、`console_checks_store.go`（checks 接线）、
  `paths.DBFile`（`state/vgatebox.db`）

## Context

P3-3 要把持久化收成 Store 接口并上 SQLite。但 Python 侧直接读 JSON：
`installer.py` 健康检查读 settings.json/basepath/password、
`distro_verify.py` 读同组 + native.json、`vps_acceptance.py compare_state`
读 settings/state/native、`pool_schedule.py` 读写 automation.json 与
refresh-state.json。意图类文件一旦进库，这些脚本全得改（Python 有 sqlite3
标准库，技术上可行，但与“先收高频低风险”的顺序相悖）。

## Decision

1. SQLite v1 只收三类调用方自有、可再生的数据：检测结果（已接线）、
   出口观察快照与 IPPure 缓存（接口就绪，接线延后）。
   意图类（state/native/settings/console/automation/pool 共享态）留在文件。
2. 接口与后端分离：`store.MapStore/DocStore` + JSON 文件后端
   （0600、整存整取，与现状语义一致）+ SQLite 后端（单连接、WAL、
   0600 建库）。切换只改构造处，调用方不动。
3. checks 接线 fail-open：库打不开回退旧 `checks.json`（`legacyChecksFile`
   读写旧形状）；旧库升级上来没有历史也无妨，结果随下一轮检测重建。
   意图类状态仍是 fail-closed，此处是特例（数据可再生），不是先例。
4. 备份：卸载/备份拷整树（含 `-wal`/`-shm`）；`shutdown` 先 flush 再
   Checkpoint + Close，保证单文件自包含。

## Consequences

- 好处：检测结果的高频合并写走 WAL 事务；单测可在 Windows 本地实跑；
  后续域（slot_health、ippure）照同一模式接线。
- 代价：二进制引入 modernc（构建慢 1–3 分钟，体积 +10MB 级）；
  旧 `checks.json` 在 SQLite 模式下不再被读（残留文件可删，备份仍会带上）。

## Invariants（改动本机制必须保持）

- Python 在读的 JSON 一律不得进库，除非对应脚本同步改为 sqlite3 读取。
- 新增 Store 域必须说明 fail-open 还是 fail-closed，并有对应单测。
- 库文件权限 0600；备份/卸载必须覆盖 `-wal`/`-shm` 或先 Checkpoint。
