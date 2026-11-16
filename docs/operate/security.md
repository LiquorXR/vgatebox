# 安全

vgatebox 以 root 运行，管理页通常暴露在公网，并保存订阅 token、第三方代理凭据和出口配置。安全边界需要按“公网管理入口 + 不可信上游节点”来设计。

## 威胁模型

| 风险 | 说明 | 缓解 |
| --- | --- | --- |
| 管理页被扫描/爆破 | 公网 IP + 固定端口 | 随机访问路径、强密码、登录节流、防火墙限制来源、HTTPS/SSH 转发 |
| 订阅地址泄露 | token 在 URL 里 | 不公开分享、泄露后重置口令、`no-store` |
| 上游节点不可信 | VPN Gate 是志愿者节点，出口能看到明文流量 | 只通过 HTTPS 访问敏感站点；不要把 VPN Gate 当匿名网络 |
| VPS 被入侵 | root 权限、状态和凭据在磁盘上 | 系统加固、SSH 密钥、最小暴露面、及时更新 |
| 第三方代理凭据泄露 | 保存了账号密码 | 私有状态权限、备份加密、不要上传备份 |
| 公网 SOCKS5 被滥用 | 有认证的 SOCKS5 仍可能被暴力尝试 | 强凭据、防火墙限制来源、不用时关闭 |
| 隧道断开导致流量直出 | 出口故障时可能回落公网 | kill switch、阻断绑定入口、不自动切换本机网络 |

vgatebox 不能保护：已被 root 攻陷的主机、恶意 VPN Gate 节点对明文流量的观察、客户端本地的恶意软件、法律和合规风险。

## 管理入口

### 访问路径与密码

- 安装时生成随机访问路径（`state/basepath`）和随机密码（`state/password`）。
- 管理地址形如 `http://<IP>:8899/<随机路径>/`，不是固定的 `/`。
- 修改密码需要当前密码；新密码至少 12 个字符。已有短密码仍可登录，但下次修改时执行新规。
- `vgatebox access` 会读取当前地址和密码；不要从旧截图复制。

### 登录与会话

- 登录成功后发 `fanout_session` Cookie，默认 12 小时；HttpOnly、SameSite=Lax。
- 通过 HTTPS（或可信代理转发的 `X-Forwarded-Proto: https`）访问时，Cookie 带 `Secure`，响应带 HSTS。
- 同一来源连续失败 8 次后锁定 2 分钟；成功登录清零；失败记录 10 分钟后重置。
- 状态变更接口只接受 POST；浏览器标记为 `Sec-Fetch-Site: cross-site` 的写请求直接拒绝。
- 登录请求体限制 8 KB；管理设置 32 KB；控制台 JSON 8 MB。

### 响应头

- 管理页和登录页：严格 CSP（`script-src 'self'`）、`X-Frame-Options: DENY`、`nosniff`、`Referrer-Policy: same-origin`。
- 管理 API 和订阅：`Cache-Control: no-store`。
- HTTPS 请求：HSTS（`max-age=31536000`）。
- 静态指纹资源：一年 `immutable`；稳定名称资源 `no-cache`。

### 反向代理

推荐用 HTTPS 反向代理或 SSH 端口转发，不要把 8899 直接暴露给全网。

```text
客户端 ──HTTPS──► 反向代理 ──HTTP──► vgatebox:8899/<路径>/
```

配置：

- `FANOUT_PUBLIC_URL=https://panel.example.com`：只填外部 origin，不要带路径；用于生成订阅和分享链接。
- 代理传给管理服务 `X-Forwarded-Proto: https`、`X-Forwarded-Host`、`X-Forwarded-Port`。
- 代理地址必须是本机/私有网段，或在 `FANOUT_TRUSTED_PROXIES` 中列出（CIDR 或 IP，逗号分隔）。
- 代理读超时至少 120 秒；入口重载可能较慢，超时太短会出现 502。
- 代理额外增加路径前缀时，把该前缀配置为管理访问路径，而不是写进 `FANOUT_PUBLIC_URL`。
- 只代理管理页面时，节点链接优先使用 `FANOUT_PUBLIC_IP` 或自动探测到的公网 IPv4；探测不到才回退到外部 origin 的主机名。

`FANOUT_PUBLIC_URL` 只影响生成的外部地址，不参与“请求是否 HTTPS”的安全判断；安全判断仍看实际连接或可信代理头。

### 防火墙

- 管理端口默认 `8899`；只对需要的来源开放，或只监听 `127.0.0.1` 并通过 SSH 转发/反向代理访问。
- 接入端口按实际使用放行；不需要的入口关闭或删除。
- 公网 SOCKS5 需要强凭据，最好限制来源 IP；不用时关闭。
- 系统防火墙和服务商安全组都要检查。

## 凭据与数据

| 数据 | 位置 | 保护 |
| --- | --- | --- |
| 管理密码 | `state/password` | 0600；目录 0700 |
| 访问路径 | `state/basepath` | 0600 |
| 订阅 token | `state/settings.json` | 0600；等同密码 |
| 第三方 SOCKS5 账号密码 | `state/native.json` 等私有状态 | 0600；不进订阅 |
| 出口 SOCKS5 凭据 | `state/state.json` | 0600；公网 SOCKS5 用 |
| OpenVPN 配置 | `state/`、`run/` | 0600；含节点信息 |
| 检测结果/画像 | SQLite `state/vgatebox.db` | 0600；含出口 IP 和国家/类型 |
| 备份 | `/var/backups/vgatebox` | 目录 0700，归档 0600；含全部凭据 |

- 工作目录、`state/`、`cache/`、`certs/`、`run/` 默认 0700。
- 订阅和 API 不返回第三方代理密码、OpenVPN 凭据和 VLESS Encryption 服务端参数。
- 日志只记录方法、路径、状态和耗时；不记录 query（可能含订阅 token）、body（可能含密码）和请求头。
- 备份包含凭据；复制到其他机器前加密，不要上传到公开仓库或不可信对象存储。

## 网络隔离

- 每条 VPN Gate 出口在自己的 network namespace 中运行 OpenVPN，主机侧通过 veth 和 iptables 转发。
- 出口断开或证据失效时，绑定入口阻断；不会自动回退本机网络。
- 内部 SOCKS5 只监听 `127.0.0.1`，供内置 Xray 使用；公网 SOCKS5 是独立的、需要显式开启的 Xray 入站。
- 第三方 SOCKS5 的流量经本 VPS 转发；上游能看到流量，选择可信供应商。
- 原始 SSTP 由客户端直连 VPN Gate，不经过本 VPS；使用官方公开的 `vpn/vpn` 凭据。

## 更新与供应链

- 在线更新只接受 GitHub Releases 的正式完整包；校验 SHA256、成员清单、版本和构建来源。
- 安装包要求 `BUILD.json` 的 `source_dirty=false`、`manager_origin=built_from_source`；来源不干净的包会被拒绝。
- Xray 二进制按归档 SHA256 校验后执行；SSTP 客户端在 CentOS 上从固定版本源码构建。
- 依赖和上游来源见 [THIRD_PARTY.md](../../THIRD_PARTY.md)。

## 加固清单

- [ ] 管理页使用 HTTPS 反向代理或 SSH 转发；不直接暴露 8899 给全网。
- [ ] 使用强密码；定期更换；泄露后立即改密码并重置订阅 token。
- [ ] 只放行必要的管理/接入端口；不需要的入口关闭。
- [ ] 公网 SOCKS5 使用强凭据，最好限制来源 IP。
- [ ] 备份加密并限制访问；不在聊天/仓库里贴订阅地址、密码或二维码。
- [ ] 保持系统和 vgatebox 更新；关注 Release Notes 的安全修复。
- [ ] 定期检查 `journalctl -u vgatebox-fanout`、`/api/metrics` 和异常登录。
- [ ] 不在 VPN Gate 出口上传输需要端到端保密且不信任出口的明文流量。

## 相关

- [安装](install.md)
- [更新](update.md)
- [容量与资源](capacity.md)
- [配置参考](../reference/configuration.md)：反向代理和环境变量
- [状态与文件](../reference/state-and-files.md)：权限和路径
- [订阅](../use/subscription.md)：token 与二维码
