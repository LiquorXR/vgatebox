# 原始 SSTP

「接入 → 原始 SSTP」检测 VPN Gate 官方候选声明的 SSTP 端点，并把通过且未过期的记录导出给支持 SSTP 的客户端。它**不是**本 VPS 的 VLESS 入口，也不能直接导入 v2rayN 的 VLESS 订阅。

## 它是什么

- 从当前 VPN Gate 候选里挑出声明了 SSTP 端点的节点。
- 在本 VPS 上临时建立 SSTP 连接，验证 TLS 证书、PPP 协商、出口 IP 和 HTTPS 内容。
- 通过后记录端点、端口、观测时间和有效期，导出为 `sstp://` 文本或 JSON。
- 客户端直接连接 VPN Gate 的 SSTP 端点；**数据不经过本 VPS**，VPS 离线不影响已保存的独立连接。

SSTP 使用官方公开的 `vpn` / `vpn` 凭据；这不是你的 VPS 凭据，也不是订阅 token。

## 检测流程

1. 从候选的 SSTP 端点数组合里选一个官方端点（域名后缀必须是 `.opengw.net` 或 `.vpngate.v4.open.ad.jp`）。
2. 先验证 TLS 证书和主机名；不接受不安全模式。
3. 在隔离的 network namespace 里用 `sstpc` + `pppd` 建立连接，固定服务器 IP，避免 DNS 变化打开意外路由。
4. 通过 `tun0` 查询出口 IP，要求是公网 IPv4 且不等于 VPS 自己的 IP。
5. 访问 HTTPS 内容做二次验证。
6. 记录延迟、国家/IP 类型（如果画像可用）、端口和有效期。

SSTP 证据与 OpenVPN 证据隔离：SSTP 结果不会复用 OpenVPN 的连接或结论，反之亦然。

## 页面操作

- 调整并发、检测周期、结果有效期和超时。
- 单条/批量检测；可以取消。
- 选择节点导出；文本格式每行一个 URI，JSON 格式带观测字段。
- 没有仍有效的实测结果时，导出返回 409，而不是给出未经验证的端点。

### 设置范围

| 设置 | 默认 | 范围 |
| --- | --- | --- |
| 并发 | 当前检测并发 | 1 到实例 `/30` 地址容量 |
| 周期 | 关闭（0） | 0–525600 分钟；0 表示不自动检测 |
| 有效期 | 60 分钟 | 1–10080 分钟 |
| 超时 | 90 秒 | 10–3600 秒 |

要求系统有 `sstpc`、`pppd`、`unshare`、`mount`、`curl`、`ip`、`iptables`。Debian/Ubuntu 由安装器安装 `sstp-client`；CentOS 首次安装从固定版本源码编译本机 RPM。缺少依赖时页面会显示 `dependency_error`。

## 导出格式

文本：

```text
sstp://vpn:vpn@vpn-example.opengw.net:443
```

JSON：

```json
{
  "schema_version": 1,
  "kind": "vpngate_vps_sstp_observations",
  "observed_by": "vps",
  "generated_at": "2026-01-01T00:00:00Z",
  "cf_status": "not_tested",
  "nodes": [
    {
      "hostname": "vpn-example.opengw.net",
      "server_ip": "203.0.113.10",
      "port": 443,
      "username": "vpn",
      "password": "vpn",
      "source_country": "JP",
      "result": { "status": "passed", "checked_at": "...", "expires_at": "..." },
      "uri": "sstp://vpn:vpn@vpn-example.opengw.net:443"
    }
  ]
}
```

Windows 可以新建系统 VPN，类型选 SSTP，填入服务器地址；端口不是 443 时按客户端要求填写。其他系统使用支持 SSTP 的客户端。

## 边界

- 只能导出 VPN Gate 官方候选声明的 SSTP 端点；不是通用 SSTP 服务器。
- 只导出通过且未过期的记录；过期后需要重新检测。
- SSTP 不是 VLESS；不能把 `sstp://` 当 VLESS 链接导入 v2rayN 的订阅。
- 客户端直连 VPN Gate，不经过本 VPS 的出口、策略和订阅；本 VPS 的出口故障不影响已保存的 SSTP 连接，反之亦然。
- SSTP 的可用性取决于官方端点；检测通过不保证长期在线。

## 相关

- [接入](access.md)：其他接入方式
- [节点池](nodes.md)：候选来源与检测
- [订阅](subscription.md)：VLESS 订阅，不含 SSTP
- [边界与兼容](../reference/limits.md)：协议矩阵
- [HTTP API](../reference/api.md)：`/api/resources/sstp/*`
