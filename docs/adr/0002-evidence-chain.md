# ADR-0002：证据链（检测结论的构成与失效）

- Status: accepted
- Date: 2026-11-06
- 范围：`vps/fanout/console_checks.go`（CheckResult/CheckRun）、`console_ttl.go`、`connect_observations.go`、
  `egress.go`、`country_evidence.go`、`console_sstp.go`（freshSstpResult）、`ippure.go`

## Context

界面要回答“这个节点现在能不能用、是什么国家、是不是住宅”，但检测是抽样、IP 画像会过期、
用户还会手填国家/备注。必须把“实测证据”和“人的标注”分开，并让每条结论自带过期条件，
否则旧结论会被当成现状展示。

## Decision

1. 证据载体统一为 `CheckResult`：`CheckedAt`（何时测）、`ExpiresAt`（何时失效）、`ProfileSHA`
   （测的是哪个配置摘要）、`Status/Code`、`ActualIP`、`Country/IPType`、`Egress`（画像快照）、
   `ObservedBy: "vps"`、`CFStatus: "not_tested"`、`Stale`。
2. 画像快照（`EgressProfile`）只有同时满足才采信国家/IP 类型：
   `IntelligenceStatus == "passed"`、未过期、`Provider/QueriedAt/RuleVersion` 非空；
   否则 `IPType` 回 `unknown`、过期则标 `stale`。手填的国家/城市/备注永远不是证据。
3. 失效规则（任一即 stale）：`CheckedAt` 为零/在未来、配置摘要变化（`profileDigest`）、
   过了 `ExpiresAt`（有效期按检测间隔与实测轮次推导，覆盖下一轮检测）；`passed` 另要求：
   公网 IPv4、`observed_by == vps`、端口 1–65535（SSTP 还要求官方端点匹配）。
4. 观测只是辅助：启动时读入的 `checks.json` 只记 `recordConnectivity`，永不把隧道恢复成 healthy；
   国家与住宅类型分别判断，`unknown` 表示未知、不能当已验证住宅。
5. SSTP 与 OpenVPN 证据隔离：`freshSstpResult` 重算 stale，不复用 OpenVPN 的连接与结论。

## Consequences

- 好处：结论可解释（界面能展示“何时测、对什么配置、何时过期”）；过期结论自动降级，不会静默转正。
- 代价：调用方必须处理 `stale`（如 `/api/console` 按当前 TTL 重标）；存储里长期留着过期条目，
  靠 `retainChecks` 与 TTL 收敛。

## Invariants（改动本机制必须保持）

- 不虚构证据：无实测即 `not_tested`/`unknown`；手填字段不得流入 `Country/IPType` 判定。
- 观测不恢复隧道健康；画像过期必须降级，不得沿用。
- 有效期按“覆盖下一轮检测”推导，手动与自动任务互斥、失败退避语义不变。
- 回归：解析/证据一致性/异常响应的 Python 单测 + Go check/settings/TTL 单测。
