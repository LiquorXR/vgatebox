# ADR-0005：更新事务（检查 → 受理 → 进程外执行 → 轮询）

- Status: accepted
- Date: 2026-11-06
- 范围：`vps/fanout/update_manager.go`（check/apply/status）、`update.go`（版本比较与 release 拉取）、
  安装入口 `vgatebox update`（进程外执行、备份与回滚的实际承担者）

## Context

更新会短暂中断连接、替换二进制与配置，失败必须能回到原部署。fanout 进程内做这件事风险极高
（自杀式替换），所以 fanout 只做“检查与受理”，真正的下载/校验/备份/替换/回滚由独立进程完成，
fanout 通过状态文件轮询进度。还要防并发更新、防版本竞态（检查完发版变了）、防在非系统实例上误触发。

## Decision

1. 三段式：`check`（GET 最新正式版：tag 必须 `^vX.Y.Z`、非 draft/prerelease、本架构有完整包 +
   checksums，返回 has_update/notes/url/supported）→ `apply`（POST 受理）→ `status`（GET 轮询）。
2. 受理门（`apply`，任一不过回 409/500 且不落任务）：已有 running 任务则拒绝；`expected_version`
   与最新不一致则要求重查（防发版竞态）；已是最新则直接返回不启动；`ready()` 限定系统安装实例
   （native 模式、工作目录为默认路径、安装入口文件齐、`systemd-run` 可用）。
3. 进程外执行：受理后写 `running/preparing` 状态（`atomicConfigWrite`），经 `systemd-run` 起一次性单元
   `vgatebox-update`（`RuntimeMaxSec=900`）跑 `vgatebox update --version <tag> --job-id <id>`，
   日志进 `/var/backups/vgatebox/update-<id>/install.log`；`launch` 失败则状态置 failed 并回 500。
4. 状态判定（`status`）：`running` 但超过 5 秒无心跳且进程指纹（pid starttime + boot_id）与
   `systemctl is-active vgatebox-update` 双通道都对不上 → 置 `interrupted`，
   文案指引“重新运行安装入口恢复或重试”；`backup_available` / `rolled_back` 由安装入口回填，
   fanout 只透传不解释。
5. 更新前备份、失败恢复、卸载保留备份的实际语义由安装入口与备份目录（`/var/backups/vgatebox/`）保证，
   见安装与备份文档；fanout 不持有回滚逻辑。

## Consequences

- 好处：fanout 永不自杀；更新任务可观测（id/phase/target/message）、可中断判定、可重入。
- 代价：状态文件是跨进程契约（字段名与命令行安装入口共享，不得擅改）；
  非系统实例（自定义工作目录）没有在线更新，只能用对应安装入口。

## Invariants（改动本机制必须保持）

- 单 flight：running 任务存在时新的 apply 必须 409；`expected_version` 竞态检查不得省略。
- 中断判定必须双通道（进程指纹 + unit 存活）且宽限 5 秒以上，避免把慢启动判死。
- 状态 JSON 字段名是跨进程契约，改名即 breaking；备份/回滚能力不得弱于“更新前必有备份”。
- 回归：`update_manager_test.go`、`update_test.go`（含中断判定矩阵）。
