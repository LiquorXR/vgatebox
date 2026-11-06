# ADR-0004：订阅格式（v2rayN 默认，Clash/Mihomo 按需）

- Status: accepted
- Date: 2026-11-06
- 范围：`vps/fanout/sub.go`（handleSub/apiSub/apiSubReset）、`sub_clash.go`（renderClash）、
  `console.go`（apiConsoleQR 与参数对齐）

## Context

客户端各异（v2rayN 吃 base64 明文链接，Clash Verge 吃 Mihomo YAML），但服务端只有一份“哪些入口可被订阅”
的事实。订阅要在免登录条件下可拉取，又不能成为信息泄露口；不同客户端表达能力不同
（如 Mihomo 写不出 VLESS + mKCP），必须显式声明省略而不是静默丢弃。

## Decision

1. 接入与鉴权：`/sub` 免登录（`Auth.Wrap` 放行），但验独立 20 字节 hex 口令（ constant-time 比较，
   口令错回 404 伪装成不存在）；仍套在访问路径之后，形成“路径 + 口令”两层。
   口令由 `apiSub` 下发、`apiSubReset` 轮换（旧地址立即失效）。
2. 输出格式（`target`）：`base64`（默认，整份一次编码）/ `links`（明文排查用）/
   `clash`|`mihomo`（Mihomo YAML）。非法 target 回 400。
3. 选择参数：`slots`（出口编号）/ `proxies`（第三方 id）/ `direct` / `vpn` / `third` / `bound` /
   `host`，与二维码接口（`apiConsoleQR`）同语义；排序固定为本机直出 → 自定义代理 → VPN Gate 出口。
4. 空订阅回 404 而不是空串：客户端遇到空订阅会清空已有节点，非 200 则保留上一份。
5. 能力缺口显式化：Mihomo 无法表示的传输（如 VLESS + mKCP）整条省略，并经
   `X-Vgatebox-Omitted` / `X-Vgatebox-Warning` 头告知条数与原因。
6. 缓存与刷新语义：`Cache-Control: no-store`、`Profile-Update-Interval: 12`（小时）、
   附件名为 fanout / vgatebox-clash.yaml。

## Consequences

- 好处：一种事实、多种表达；客户端行为可预测（404 保留旧订阅、省略可计数）。
- 代价：免登录端点 forever 存在，口令强度与轮换是唯一防线；新增传输前必须先确认两种格式的表达能力。

## Invariants（改动本机制必须保持）

- 口令校验恒定时间、失败伪装 404；管理凭证永不出现在订阅内容或观测记录里。
- 空结果必须非 200；省略必须计数到响应头，不得静默。
- 新增 `target` 或选择参数时，`/sub` 与 `/api/console/qr` 同步。
- 回归：`sub_test.go`、`sub_clash_test.go`（13 个「传输 × 安全层」组合 + 拒绝路径 + 固定 Mihomo 内核验证）。
