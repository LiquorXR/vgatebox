# 边界与兼容

这一页集中列出 vgatebox 支持什么、不支持什么、哪些是已验证的。功能页只描述用法，边界以本页为准。

## 系统与架构

| 项目 | 状态 |
| --- | --- |
| Debian 12 / 13 | 自动安装、更新、卸载通过 |
| Ubuntu 22.04 / 24.04 | 自动安装、更新、卸载通过 |
| CentOS Stream 9 / 10 | 保留安装支持；不纳入自动测试矩阵，独立 VPS 验收尚未完成 |
| Linux x86_64（amd64） | 支持 |
| ARM64 / 其他架构 | 不支持 |
| CentOS 7 / 8、Stream 8 | 不支持 |
| OpenVZ 等受限虚拟化 | 不支持 |
| 非 systemd 系统 | 不支持 |
| 无 root / 无 TUN / 无 network namespace | 不支持 |

CentOS 首次安装会从固定版本（`sstp-client 1.0.20`）源码编译 SSTP 客户端，耗时和内存需求高于 Debian/Ubuntu。测试矩阵不覆盖独立 VPS 的内核、SELinux、服务商防火墙和网络策略；这些需要在真实环境验收。

## 客户端

| 客户端 | 状态 | 说明 |
| --- | --- | --- |
| v2rayN | 支持 | 默认 Base64 订阅；可导入 VLESS + mKCP |
| Clash Verge / Rev（Mihomo） | 支持 | 使用 Clash/Mihomo 格式；mKCP 会被跳过 |
| Mihomo 内核 | 支持 | REALITY、VLESS Encryption 需要较新版本 |
| 其他 VLESS 客户端 | 视实现 | 需要支持对应传输/安全层；旧客户端可能不识别 `encryption`、REALITY 新字段或 `xhttp` |
| Windows 系统 SSTP | 支持 | 用「原始 SSTP」导出的 `sstp://` 参数 |
| 通用 SOCKS5 客户端 | 支持 | 用 VPS SOCKS5 或第三方 SOCKS5；不是订阅 |

没有官方 GUI 客户端；vgatebox 只提供管理页和订阅，客户端使用你选择的产品。

## 协议

### 内置 Xray

- 只提供 **VLESS**；不支持 VMess、Trojan、Hysteria2 等协议。
- 传输：TCP / RAW、WebSocket、gRPC、HTTPUpgrade、XHTTP、mKCP。
- 安全层：`none`、TLS、REALITY。
- REALITY 只支持 TCP、XHTTP、gRPC；WebSocket/HTTPUpgrade/mKCP 不支持 REALITY。
- `xtls-rprx-vision`：VLESS + TCP + TLS/REALITY，或启用 VLESS Encryption 后任意传输。
- VLESS Encryption：X25519 或 ML-KEM-768；启用后链接带 `encryption=`，旧客户端可能无法解析。
- 自动入口默认：VLESS + VLESS Encryption（X25519）+ RAW/TCP + REALITY + Vision，REALITY 目标默认 `www.tesla.com`。
- 无安全层（`security=none`）只建议用于私网；公网建议 TLS/REALITY 或至少 VLESS Encryption。

### 传输/安全矩阵

| 传输 | none | TLS | REALITY |
| --- | --- | --- | --- |
| TCP / RAW | 支持 | 支持 | 支持 |
| XHTTP | 支持 | 支持 | 支持 |
| gRPC | 支持 | 支持 | 支持 |
| WebSocket | 支持 | 支持 | 不支持 |
| HTTPUpgrade | 支持 | 支持 | 不支持 |
| mKCP | 支持（UDP） | 支持 | 不支持 |

### 订阅表达

- v2rayN/Base64：VLESS 链接，支持全部传输/安全组合。
- Clash/Mihomo：VLESS + TCP/WS/HTTPUpgrade/gRPC/XHTTP；**不表达 mKCP**，整条代理跳过。
- Clash 输出使用 `packet-encoding: xudp`、`udp: true`；REALITY 显式开启 `support-x25519mlkem768`。
- 订阅只输出 `up` 出口的已启用入站；`degraded`、`blocked`、`waiting_for_nodes`、`failed`、`stopped` 不输出。
- 未绑定出口的入站默认不进入订阅（`bound=0` 才包含）。

## 网络与数据面

- 当前代理链路面向 **TCP / IPv4**；IPv6 未作为验收范围。
- UDP 在 IPv4 下全链路启用；v2rayN 需要客户端开启 UDP；Mihomo 使用 `udp: true`/`xudp`。
- 第三方 SOCKS5 的 UDP 能力取决于上游，不保证。
- 每条 VPN Gate 出口在独立 network namespace 中运行 OpenVPN；主机侧有 veth、iptables 转发和 kill switch。
- 出口故障时绑定入口阻断，**不会自动回退本机网络**。
- VPS SOCKS5 是显式开启的公网入口，需要认证和防火墙；内部 SOCKS5 只监听 `127.0.0.1`。
- 原始 SSTP 由客户端直连 VPN Gate，不经过本 VPS。

## 上游与证据

- VPN Gate 节点由志愿者提供，可用数量、国家、速度和在线时间随时变化。
- 候选数量不等于可用出口数；检测通过只代表那一次检测，有 30 分钟有效期。
- 国家与 IP 类型分别判断；`unknown` 表示缺少分类证据，不能称为住宅。
- 手填国家、城市、备注不是证据；不会用于连通性、国家或住宅判定。
- 出口策略要求 ISP/ASN 时，必须有完整画像；缺少字段不会被当作满足。
- 不保证“住宅 IP”或“家宽”；分类服务不可用时显示 `unknown`。
- 镜像后备只提供原始 CSV，不采用镜像的检测结论；镜像快照默认最大 24 小时。

## 容量

- `maximumSlots = 16384` 是实例地址空间上限，不是单机建议值。
- 建议每 Slot 预留 64 个 fd、约 32 MiB 内存；启动日志的容量预检只提示，不修改 `-max`。
- 实际容量受 OpenVPN 进程、netns/veth、内核内存、iptables、端口区间和上游质量影响。
- 内部 SOCKS5 端口默认 `20000–59999` 随机；设置 `FANOUT_SLOT_PORT_BASE` 后为基址–65535。
- 管理端口默认 `8899`；接入端口按入口决定，需要单独放行。
- 检测与正式建连共享并发预算；默认检测并发 2，正式出口恢复优先。

详见 [容量与资源](../operate/capacity.md)。

## 管理与运维

- 一个工作目录只允许一个实例；第二个实例启动失败。
- 在线更新只适用于系统安装实例（工作目录 `/var/lib/vgatebox/fanout`、内置 Xray、存在系统安装入口）；自定义目录用对应安装入口。
- 网页更新只使用 GitHub Releases 的正式版；不使用 draft/prerelease。命令行显式版本也应选择正式版。
- 备份只覆盖本项目受管文件，不是整机镜像；重装系统前需另存。
- 卸载默认保留备份；`--purge` 才会删除；不删除外部面板、共享依赖和全局 IP 转发。
- 不支持 3x-ui、xray-cf-lite 等外部面板；不会导入它们的数据库或配置。`panel_mode` 非空且不是 `native` 时，服务拒绝启动。

## 安全边界

- vgatebox 不保护已被 root 攻陷的主机，也不能防止恶意 VPN Gate 节点观察明文流量。
- 管理页、订阅 token、第三方代理凭据、备份都含私密信息；需要 HTTPS/SSH 转发、强密码和访问限制。
- 公网 SOCKS5 有被滥用风险；不用时关闭，最好限制来源 IP。
- 日志不记录 query/body/请求头，但服务端仍能访问状态文件；磁盘访问控制很重要。
- 合法合规使用由部署者负责；VPN Gate 节点由第三方提供，使用范围依其说明。

## 已验证与未验证

**已验证（仓库 CI / 自动化）**

- Debian 12/13、Ubuntu 22.04/24.04 的安装、更新、卸载。
- API 路由契约 62/62。
- Go/Python/Vitest/Playwright 测试矩阵。
- Clash 配置用固定 Mihomo 内核验证可表达的 VLESS 组合。
- Debian 12 隔离安装测试用真实订阅连接本机 VLESS（TCP + REALITY + VLESS Encryption + Vision），校验 HTTPS 出口且失效时不回落 DIRECT。

**未验证 / 需要真实 VPS 验收**

- CentOS Stream 的独立 VPS 安装与更新（保留支持，不在自动矩阵）。
- 各服务商防火墙、SELinux、内核参数和网络策略。
- 所有 Clash Verge/Rev GUI 版本；只验证了固定 Mihomo 内核。
- 所有客户端的旧版本；VLESS Encryption/REALITY 需要较新内核。
- 大量出口下的长期稳定性；容量建议只是保守估算。
- 原始 SSTP 在不同客户端/系统上的兼容性。

## 相关

- [产品概览](../start/overview.md)
- [容量与资源](../operate/capacity.md)
- [订阅格式](subscription-format.md)
- [安全](../operate/security.md)
- [测试](../develop/testing.md)
