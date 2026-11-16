# vgatebox

在自己的 Linux VPS 上，把 VPN Gate 候选、第三方 SOCKS5 和 VPS 本机网络整理成一条可订阅的出口；客户端用 v2rayN 或 Clash/Mihomo 导入后即可使用。

vgatebox 不依赖第三方订阅转换服务，也不需要 3x-ui、xray-cf-lite、Cloudflare 或 GitHub 账号。管理进程、内置 Xray、候选池和隧道都在自己的 VPS 上运行。

## 能做什么

- **VPN Gate 出口**：从官方候选创建隔离的 OpenVPN 隧道，按国家、IP 类型、ISP/ASN 和延迟策略选点；支持换 IP、调整数量和删除单个出口。
- **自有代理**：添加固定 SOCKS5 地址、账号和密码，经本机内置 Xray 转成 VLESS 接入。
- **本机网络**：用 VPS 公网 IP 直出，适合首次验证客户端或不需要额外出口的场景。
- **统一订阅**：v2rayN（Base64）和 Clash/Mihomo（YAML）两种格式；按本机网络、第三方代理、VPN Gate 排序，显示国家和备注。
- **网页管理**：总览、出口、节点池、订阅、接入、设置；支持更新、备份、失败恢复和完整卸载。
- **原始 SSTP**：检测并导出 VPN Gate 的 SSTP 参数，供支持 SSTP 的客户端直接使用。

## 安装

需要 **root**、**Linux x86_64**、**systemd**，以及可用的 TUN / network namespace。支持 Debian 12/13、Ubuntu 22.04/24.04 和 CentOS Stream 9/10；CentOS 首次安装会本机编译 SSTP 客户端，耗时和内存需求更高。

以下命令自动选择 APT 或 DNF，安装最新正式版：

```sh
if command -v apt-get >/dev/null; then apt-get update && apt-get install -y ca-certificates curl; else dnf -y install ca-certificates && { command -v curl >/dev/null || dnf -y install curl; }; fi &&
curl -fsSL https://github.com/LiquorXR/vgatebox/releases/latest/download/install.sh -o /tmp/vgatebox-install.sh && bash /tmp/vgatebox-install.sh && vgatebox access
```

需要固定版本时，把 `bash /tmp/vgatebox-install.sh` 改为 `bash /tmp/vgatebox-install.sh --version vX.Y.Z`。

安装完成后会显示管理地址和密码，之后可随时查看：

```sh
vgatebox access
```

在浏览器打开管理地址登录。防火墙需放行管理端口（默认 `8899`）和实际使用的接入端口；系统防火墙与服务商防火墙都要检查。

## 第一次成功

1. 登录管理页；首次安装默认开启「VPS 本机网络」。
2. 在「订阅」选择格式：v2rayN 用默认格式，Clash Verge / Rev 用 Clash/Mihomo 格式。
3. 复制订阅链接，在客户端新建订阅或远程配置并更新。
4. 选择「VPS 本机网络」验证能上网；需要 VPN Gate 时在「出口」等待「已就绪」后再次更新订阅。

首次没有保存出口数量、也没有旧出口时，取得候选后默认创建 6 个 VPN Gate 出口；已有数量（包括 0）会保留。完整步骤见 [快速开始](docs/start/quickstart.md)。

## 更新与卸载

更新会短暂中断连接，保留管理地址、密码、订阅和出口设置；失败时尝试恢复原部署。

```sh
vgatebox check-update
vgatebox update
vgatebox update-status
```

卸载默认保留备份，不会删除其他程序或共享系统依赖：

```sh
vgatebox uninstall
```

需要同时删除备份和可确认归属的 fanout 安装时，先预览再执行：

```sh
vgatebox uninstall --dry-run --purge --legacy
vgatebox uninstall --purge --legacy
```

卸载需要确认；自动化使用 `--yes`。备份位于 `/var/backups/vgatebox/`，重装系统前请另存。详见 [更新](docs/operate/update.md) 和 [卸载](docs/operate/uninstall.md)。

## 边界

- VPN Gate 节点由志愿者提供，可用数量和国家随时变化；候选数量不等于可用出口数。
- VPN Gate 或第三方代理故障时，对应连接阻断，**不会自动改用 VPS 本机网络**。
- 国家与住宅类型分别判断；`unknown` 表示缺少分类证据，不能称为已验证住宅 IP。手填国家、城市和备注不是检测证据。
- 当前链路面向 TCP / IPv4；内置 Xray 只支持 VLESS，Mihomo 无法表示 VLESS + mKCP。
- 管理地址、订阅和备份含私密信息；公网管理建议使用 HTTPS 反向代理或 SSH 转发。

完整能力和兼容范围见 [边界与兼容](docs/reference/limits.md)。

## 文档

- 第一次使用：[快速开始](docs/start/quickstart.md) · [产品与概念](docs/start/concepts.md)
- 日常操作：[出口](docs/use/exits.md) · [接入](docs/use/access.md) · [订阅](docs/use/subscription.md) · [节点池](docs/use/nodes.md)
- 部署运维：[安装](docs/operate/install.md) · [更新](docs/operate/update.md) · [安全](docs/operate/security.md) · [排障](docs/operate/troubleshooting.md)
- 参考手册：[配置](docs/reference/configuration.md) · [CLI](docs/reference/cli.md) · [HTTP API](docs/reference/api.md)
- 开发与设计：[架构](docs/develop/architecture.md) · [测试](docs/develop/testing.md) · [ADR](docs/adr/README.md)

文档首页：[docs/README.md](docs/README.md)。

## 许可

核心工具采用 [GPL-3.0-only](LICENSE)。依赖许可、上游来源和复用边界见 [THIRD_PARTY.md](THIRD_PARTY.md)。
