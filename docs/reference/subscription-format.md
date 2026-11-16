# 订阅格式

`/sub` 支持三种输出：`base64`（默认）、`links`（明文）和 `clash`（Mihomo YAML）。三者使用同一份接入列表，只是编码和字段表达不同。

## 通用行为

- 只输出**可服务**的接入：本机网络按启用状态；第三方代理需启用且“加入订阅”；VPN Gate 只输出 `up` 出口中已启用的入站。
- 默认顺序：本机网络 → 第三方代理 → VPN Gate；同一类内按入站/代理 ID 排序。
- 空订阅返回 `404`，避免客户端清空已有节点。
- 响应头：

  ```text
  Cache-Control: no-store
  Profile-Update-Interval: 12
  ```

- 订阅 token 错误返回 `404`，不暴露“这里有订阅服务”。
- 非法 `target`/`slots`/`proxies`/`direct`/`vpn`/`third` 参数返回 `400`。
- 订阅地址仍在随机访问路径之后，例如 `/<basepath>/sub?token=...`。

## Base64（默认）

正文是整份链接列表的 Base64，不是逐行编码：

```text
base64( vless://...\nvless://...\n... )
```

- `Content-Type: text/plain; charset=utf-8`
- `Content-Disposition: inline; filename=fanout`
- v2rayN 和多数通用客户端使用这个格式。

## Links（明文）

`target=links` 返回用换行分隔的 VLESS 链接，便于排查和手工导入。没有 Base64 包装。

## VLESS 链接字段

示例：

```text
vless://<uuid>@203.0.113.10:30001?type=tcp&security=reality&sni=www.tesla.com&fp=chrome&pbk=<公钥>&sid=<短ID>&flow=xtls-rprx-vision&encryption=<客户端加密参数>#%F0%9F%87%AF%F0%9F%87%B5%20%E6%97%A5%E6%9C%AC%20%C2%B7%2001
```

| 参数 | 出现条件 | 含义 |
| --- | --- | --- |
| `type` | 总是 | `tcp`、`ws`、`grpc`、`httpupgrade`、`xhttp`、`kcp`（mKCP 在链接里写作 `kcp`） |
| `security` | 总是 | `none`、`tls`、`reality` |
| `encryption` | 总是 | `none` 或 VLESS Encryption 的客户端参数 |
| `flow` | 启用 Vision | `xtls-rprx-vision` |
| `sni` | TLS/REALITY | 证书域名或 REALITY serverName |
| `fp` | REALITY | 客户端指纹，默认 `chrome` |
| `pbk` | REALITY | REALITY 公钥 |
| `sid` | REALITY | short ID |
| `pcs` | 自签 TLS | 证书 SHA256 指纹，客户端据此固定信任 |
| `path` | WS/HTTPUpgrade/XHTTP | 请求路径 |
| `host` | WS/HTTPUpgrade/XHTTP | HTTP Host |
| `mode` | XHTTP | 固定 `auto`，与服务端一致 |
| `serviceName` | gRPC | 服务名 |
| `mtu` / `tti` | mKCP 自定义值 | KCP 参数；省略时用客户端默认 |

不包含：服务端 REALITY 私钥、VLESS Encryption 服务端 `decryption`、第三方 SOCKS5 账号密码、OpenVPN 凭据。

## Clash/Mihomo

`target=clash` 返回块状 YAML，结构：

```yaml
mixed-port: 7890
allow-lan: false
mode: rule
log-level: warning
ipv6: false
profile:
  store-selected: true
proxies:
  - name: "🇯🇵 日本 · 01"
    type: vless
    server: 203.0.113.10
    port: 30001
    udp: true
    uuid: "..."
    flow: xtls-rprx-vision
    packet-encoding: xudp
    tls: true
    servername: www.tesla.com
    client-fingerprint: chrome
    skip-cert-verify: false
    reality-opts:
      public-key: "..."
      short-id: "..."
      support-x25519mlkem768: true
    encryption: "..."
    network: tcp
proxy-groups:
  - name: vgatebox
    type: select
    proxies:
      - "🇯🇵 日本 · 01"
rules:
  - MATCH,vgatebox
```

### 字段映射

| VLESS 链接 | Mihomo 字段 |
| --- | --- |
| `type=tcp/raw` | `network: tcp` |
| `type=ws` | `network: ws` + `ws-opts.path` / `headers.Host` |
| `type=httpupgrade` | `network: ws` + `ws-opts.v2ray-http-upgrade: true` |
| `type=grpc` | `network: grpc` + `grpc-opts.grpc-service-name` |
| `type=xhttp` | `network: xhttp` + `xhttp-opts.path/host/mode` |
| `security=tls` | `tls: true`、`servername`、`client-fingerprint`、`alpn`、`fingerprint`（`pcs`） |
| `security=reality` | `reality-opts.public-key`、`short-id`、`support-x25519mlkem768: true` |
| `flow` | `flow`（只接受 `xtls-rprx-vision`） |
| `encryption` | `encryption`（`none` 省略） |
| 所有 VLESS | `packet-encoding: xudp`、`udp: true` |

### 不支持的项

- **VLESS + mKCP**：Mihomo 无法表达，整条代理会被跳过，不进入 Clash YAML。
- 其他非 VLESS 协议：当前不会生成；遇到会返回 400。
- 未知 `flow`、无效 REALITY 公钥/短 ID、无效证书指纹：返回 400，不生成半份配置。

跳过 mKCP 时响应头会带：

```text
X-Vgatebox-Omitted: <数量>
X-Vgatebox-Warning: some entries were omitted because Mihomo cannot represent their transport
```

如果所有条目都被跳过，返回 400 `没有 Mihomo 可表达的节点`。

### 命名

- 节点名来自 VLESS 链接的 fragment（订阅里的备注）。
- 重名会追加 ` · 2`、` · 3`，保证 Mihomo 不丢节点。
- 空名回退为 `节点 01`、`节点 02`。
- `vgatebox` 是保留的策略组名；`DIRECT`、`REJECT`、`GLOBAL` 也不会被节点占用。

## 订阅范围

见 [订阅](../use/subscription.md)。要点：

- `slots` 只选出口，设置后默认不含本机网络。
- `bound=0` 才包含未绑定出口的入站。
- `direct=0/1` 控制本机网络。
- `vpn=0/1` 控制 VPN Gate。
- `third=0/1` 控制第三方代理；`proxies` 只选指定 ID。
- `host` 覆盖链接中的连接地址；默认使用公网 IP/请求主机。

## 客户端兼容

| 客户端 | Base64 | Clash | 备注 |
| --- | --- | --- | --- |
| v2rayN | 支持 | — | 默认格式；mKCP 可导入 |
| Clash Verge / Rev | — | 支持 | 需要较新的 Mihomo；mKCP 跳过 |
| Mihomo 内核 | — | 支持 | REALITY/VLESS Encryption 需要较新版本 |
| 通用客户端 | 多数支持 | 视实现 | 不支持 VLESS Encryption 的客户端会解析失败 |

旧客户端可能不认识 `encryption`、REALITY 新字段或 `xhttp`；遇到导入失败时先升级客户端。

## 错误码

| 状态 | 场景 |
| --- | --- |
| 400 | `target` 非法、参数非法、Clash 无法表达全部条目 |
| 401 | 管理 API 未登录（`/sub` 不走登录，使用 token） |
| 404 | token 错误、没有可订阅节点 |
| 502 | 生成链接失败（内置 Xray 不可用等） |
| 500 | 订阅口令未就绪 |

## 相关

- [订阅](../use/subscription.md)：页面操作和客户端配置
- [高级接入](../use/inbounds.md)：VLESS 传输/安全组合
- [接入](../use/access.md)：本机网络和第三方代理
- [HTTP API](api.md)：`/sub`、`/api/sub`、`/api/sub/reset`
- [边界与兼容](limits.md)：客户端和协议限制
