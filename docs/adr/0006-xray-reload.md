# ADR-0006：Xray 不做动态（API）重载，保留整体进程重启 + 无变去重

- Status: accepted
- Date: 2026-11-06
- 范围：`vps/fanout/native_transaction.go`（apply）、`native_xray.go`（buildXrayConfig/xrayProc）、
  `config/xray-release.json`（pin v26.10.31）、P1-7 去重（`procLive` + 逐字节比对）

## Context

`Native.apply` 每次变更都整进程重启 Xray，所有 Slot 的已有客户端连接被连带中断。
P2-1 时间盒预研三条路（均为 v26.10.31 一手证据）：

1. Xray 动态 API：`app/proxyman/command/command.proto` 的 HandlerService 有
   Add/Remove/AlterInbound、Add/Remove/AlterOutbound、List；
   `app/router/command/command.proto` 的 RoutingService 有
   AddRule/RemoveRule/ListRule（含 SubscribeRoutingStats/TestRoute 等只读项）。
   即入站、出站、路由三者都有动态增删接口。
2. 双进程蓝绿 + `SO_REUSEPORT`：Xray 监听栈不支持套接字交接，
   两实例无法共享同一批固定端口；在途 TCP 在旧进程被杀时必断，无缝切换不成立。
3. P1-7 去重已落地：暂存与磁盘 `xray.json` 逐字节一致且进程是本实例拉起的
   （`procLive`）则跳过 restart，文件提交与失败语义不变。

## Decision

不采用动态 API 重载，保留“整体进程重启 + 无变去重”：

1. 路由语义冲突：动态 AddRule 追加在静态规则之后，而本项目每个入站都有一条
   显式规则（未绑定即 `block`，见 `buildXrayConfig`）。改用动态路由必须重排静态规则，
   把兜底 block 后移——一旦动态规则缺失/顺序错位即 fail-open，
   与“未绑定阻断”的 fail-closed 安全模型直接冲突。
2. 依赖爆炸：调 API 要引入 gRPC + protobuf + xray-core API 类型（其版本必须与
   pin 的二进制 v26.10.31 严格一致），`go.mod` 从 2 个外部依赖膨胀，
   与“Xray 是外部二进制、本项目只做编排”的边界相悖；还要在 xray.json 里常开
   api/stats 服务与本地 API 入站，新增本机攻击面。
3. 事务重做一遍：动态多步（加出站→加入站→加规则）需要 gRPC 上的补偿回滚，
   等于把 `native_transaction.go` 的 journal 语义在网络上再实现一次，
   还要处理重启后动态态丢失的漂移对账（List* 三路比对）。
4. 收益已收窄：P1-7 之后剩下的重启全是真实配置变更（Slot 增删、入站 CRUD、
   凭据轮换、第三方/直出变更），多为用户 deliberate 操作；健康抖动等高频
   无变 apply 已收敛为 no-op。残余的 ~1s 中断不值得上述代价。
5. 连窄口子也没有：HandlerService 在 v26.10.31 没有 AddUser/RemoveUser RPC
   （只有残留 message 定义），凭据轮换等客户端级操作同样走不了动态通道。

## Consequences

- 好处：无新依赖、无新攻击面、无 fail-open 风险；重启语义保持“全量、可验证、可回滚”。
- 代价：真实配置变更时全 Slot 短暂中断（现状，已在 `vps/README.md` 明确告知用户）。
- UI 必须如实提示“保存入站配置会短暂中断所有连接”（FRONTEND.md 已有对应条目）。

## Invariants（改动本机制必须保持）

- 任何绕过整进程重启的方案，必须先证明路由 fail-closed 不变（未绑定入站永不可达），
  并给出动态态漂移对账与多步回滚设计，否则不予接受。
- `procLive` + 逐字节比对的去重语义不变：首刷/崩溃后/配置变化必重启，
  回归见 `native_dedup_test.go`（首刷1次、无变0次、改端口2次）。
- Xray 大版本升级时重做一次本预研（API 语义可能变化），结论过期不沿用。
