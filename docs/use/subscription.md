# 订阅

订阅把可用的接入聚合成一条客户端可导入的链接。出口增删后，客户端更新订阅即可看到变化，不需要重新配置地址。

## 地址结构

```text
http://<VPS公网IP>:8899/<访问路径>/sub?token=<订阅口令>
```

- `<访问路径>` 是安装时随机生成的，不是固定的 `/`；`vgatebox access` 会显示完整管理地址。
- `token` 是独立的订阅口令，等同于密码；它和登录密码不是同一个。
- 订阅不需要登录，但仍在访问路径之后：猜不到路径 + 猜不到 token 才能访问。
- 重置口令后旧地址立即失效，已订阅的客户端需要重新导入。

## 选择格式

「订阅」页的「客户端格式」有两项：

| 选项 | 输出 | 适合 |
| --- | --- | --- |
| `v2rayN / 通用节点列表` | Base64 编码的 VLESS 链接列表 | v2rayN、多数通用客户端 |
| `Clash Verge / Rev（Mihomo）` | Mihomo YAML（proxies + proxy-groups + rules） | Clash Verge / Rev、Mihomo 内核 |

Clash 订阅会跳过 Mihomo 无法表示的入口（当前是 VLESS + mKCP），并在响应头里给出提示；这类入口仍保留在 v2rayN 格式。REALITY 和 VLESS Encryption 需要较新的 Mihomo 内核。

## 选择范围

「订阅」页可以分别控制：

- **VPS 本机网络**：启用且勾选时输出。
- **VPN Gate**：默认全选；只输出**已就绪**出口中**已启用**的入站。取消全选后可逐个出口选择。
- **自定义代理**：包括今后新增的第三方 SOCKS5；代理本身还需要启用并开启「加入订阅」。

默认顺序是 **本机网络 → 自定义代理 → VPN Gate**。同一出口有多个入站时按入站 ID 排序；VPN Gate 节点名默认是“国家 · 编号”，也可以自定义。

订阅为空时服务端返回 `404`，而不是空正文：客户端遇到空订阅可能清空已有节点，非 200 会保留上一份。

## 查询参数

界面会拼好地址；需要手工构造或做自动化时，可以使用以下参数：

| 参数 | 取值 | 作用 |
| --- | --- | --- |
| `token` | 订阅口令 | 必填；错误时返回 404 |
| `target` | `base64`（默认）、`links`、`clash` | 输出格式；`links` 是明文链接，便于排查 |
| `host` | 主机名或 IP | 覆盖链接里的连接地址；默认使用公网 IP/请求主机 |
| `slots` | `1,2,3` | 只选指定出口；设置后默认不再包含本机网络 |
| `bound` | `0` / `1` | `0` 表示把未绑定出口的入站也放进订阅；默认只放已绑定的 |
| `direct` | `0` / `1` | 是否包含本机网络 |
| `vpn` | `0` / `1` | `0` 表示不包含 VPN Gate |
| `third` | `0` / `1` | 是否包含第三方代理 |
| `proxies` | `1,2` | 只选指定第三方代理；会强制包含第三方 |

示例：

```text
# 只导出两个出口的明文链接
http://<IP>:8899/<路径>/sub?token=<token>&target=links&slots=1,2

# 不要本机网络，只保留 VPN Gate
http://<IP>:8899/<路径>/sub?token=<token>&direct=0

# Clash 格式
http://<IP>:8899/<路径>/sub?token=<token>&target=clash
```

参数由服务端校验；非法值返回 400，不会静默忽略。完整字段和 Clash 映射见 [订阅格式](../reference/subscription-format.md)。

## 二维码

「订阅」页的二维码由服务端生成，内容与当前格式/范围的订阅地址一致。二维码包含 token，截屏或转发等同于泄露订阅口令；不要发到公开群组。

需要单独获取二维码图片时，使用登录态请求：

```text
GET /<访问路径>/api/console/qr?target=clash
```

参数与 `/sub` 一致，返回 `image/png`。

## 客户端配置

### v2rayN

1. 「订阅」页保持 `v2rayN / 通用节点列表`。
2. 点「复制订阅」。
3. 在 v2rayN 中新建订阅，粘贴地址并更新。
4. 选择 `VPS 本机网络` 或已就绪的 VPN Gate 出口，启用系统代理/路由模式。
5. 打开网页验证；需要 UDP 时在客户端启用 UDP。

### Clash Verge / Rev

1. 「订阅」页选择 `Clash Verge / Rev（Mihomo）`。
2. 点「复制订阅」。
3. 在 Clash Verge / Rev 中新建远程配置，粘贴地址并更新。
4. 选择 `vgatebox` 策略组或具体节点。
5. 打开网页验证。

不需要第三方订阅转换服务；Clash 配置由本 VPS 直接生成。

## 更新时机

- 新增/删除出口、切换出口状态、修改订阅范围后，客户端需要**主动更新订阅**。
- 节点换 IP 时，入站端口和客户端凭据保持不变；通常不需要重新导入，更新订阅即可看到新名称/国家。
- 订阅响应带 `Profile-Update-Interval: 12`（小时），客户端可据此自动拉取；具体行为取决于客户端。
- 管理 API 的 JSON 和订阅响应都带 `Cache-Control: no-store`，避免凭据进入浏览器缓存。

## 安全

- 订阅 token 等同密码；泄露后立即在「订阅」页「重置口令」。
- 订阅地址含访问路径和 token；不要贴在公开仓库、论坛或截图里。
- 反向代理终止 TLS 时，订阅地址需要让客户端可达；`FANOUT_PUBLIC_URL` 只影响生成的外部 origin，不参与安全判断。详见 [安全](../operate/security.md)。
- 订阅内容只含客户端连接所需信息，不含 VPN Gate 的 OpenVPN 凭据和第三方 SOCKS5 的账号密码。

## 相关

- [产品与概念](../start/concepts.md)：订阅为什么只输出已就绪出口
- [出口](exits.md)：如何让出口进入「已就绪」
- [接入](access.md)：本机网络和第三方代理的启用/订阅开关
- [订阅格式](../reference/subscription-format.md)：链接字段、Clash 映射和限制
- [HTTP API](../reference/api.md)：`/sub`、`/api/sub`、`/api/sub/reset`
