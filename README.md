# vgatebox

在自己的 VPS 上管理 VPN Gate、第三方 SOCKS5 和本机直出，通过一个订阅链接在 v2rayN 等客户端使用。

- **多种出口**：自动连接 VPN Gate，也可手动添加固定 SOCKS5 地址、账号和密码。
- **网页管理**：管理出口、接入、检测并发、周期任务、国家策略与订阅范围。
- **统一订阅**：支持 v2rayN 与 Clash/Mihomo 格式，按本机直出、自定义代理、VPN Gate 排序，显示国家和自定义备注。
- **独立运行**：唯一入口后端是内置 Xray；不再支持 3x-ui / xray-cf-lite 等外部面板，也不需要 Cloudflare 或 GitHub 账号。
- **更新与恢复**：支持网页更新、更新前备份、失败恢复及完整卸载。
- **原始 SSTP**：检测并导出 VPN Gate SSTP 资源，供支持 SSTP 的客户端直接使用。

## 安装

需要 root、Linux x86_64、systemd 和可用的 TUN/network namespace。支持范围与各系统测试结果见 [系统兼容性](docs/LINUX_SUPPORT.md)。

安装入口始终使用最新正式版，版本与发布记录见 [Releases](https://github.com/LiquorXR/vgatebox/releases/latest)。统一安装入口支持 Debian 12/13、Ubuntu 22.04/24.04 和 CentOS Stream 9/10。六个系统已通过自动安装、更新及卸载测试；CentOS 独立 VPS 验收尚未完成。CentOS 7/8（含 Stream 8）不在支持范围。

以上系统均以 root 执行同一段命令，自动选择 APT 或 DNF：

```sh
if command -v apt-get >/dev/null; then apt-get update && apt-get install -y ca-certificates curl; else dnf -y install ca-certificates && { command -v curl >/dev/null || dnf -y install curl; }; fi &&
curl -fsSL https://github.com/LiquorXR/vgatebox/releases/latest/download/install.sh -o /tmp/vgatebox-install.sh && bash /tmp/vgatebox-install.sh && vgatebox access
```

需要固定版本时，把 `bash /tmp/vgatebox-install.sh` 改为 `bash /tmp/vgatebox-install.sh --version vX.Y.Z`。

安装完成后显示随机生成的管理地址和密码。之后可随时查看：

```sh
vgatebox access
```

在浏览器打开管理地址并登录。防火墙需放行管理端口（默认 `8899`）及实际使用的接入端口；系统和服务商的防火墙都需检查。

CentOS 首次安装会编译 SSTP 客户端，耗时和内存需求高于 Debian / Ubuntu，请等待依赖安装与构建完成。

首次安装默认创建并开启本机直出，可直接导入订阅；已有安装的开关和客户端身份保留。VPN Gate 来源暂不可达时仍可完成安装并登录，候选由后台或定时器获取；也可添加第三方 SOCKS5。VPN Gate 出口等待候选，不自动回落直出。

第三方镜像后备默认开启，可在候选页的「来源设置」关闭；已有明确保存的关闭选择保留。仍由自己的 VPS 优先请求原来源，失败后下载并校验镜像原始 CSV，再自行检测节点与真实出口；不采用镜像的检测结论。页面区分本机获取时间和镜像声明的抓取时间。详见 [候选来源](docs/POOL_SOURCE_FALLBACK.md)。

## 首次使用

1. 刷新候选
2. 等待就绪
3. 复制订阅
4. 导入使用

可先用默认的“VPS 本机直出”测试客户端接入。未设置数量且无旧 Slot 时，取得候选后自动创建 6 个 VPN Gate 出口并按当前并发连接，无需先设置数量或检测全部候选；已有数量（含 0）和旧出口保留。可按需调整数量或添加自己的 SOCKS5，出口就绪后更新客户端订阅。候选刷新显示等待时间、完成结果和上次成功获取时间。详细操作见 [首次使用](docs/FIRST_USE.md)。

「节点池 → 周期任务」可分别设置来源刷新和候选检测，默认每 60 分钟一次，最低 30 分钟，也可关闭；检测沿用用户设置的并发。同一自动任务不重叠，刷新失败保留旧候选。所有获取与检测均由自己的 VPS 执行。

使用 Clash Verge 时，在「订阅」选择 Clash/Mihomo 格式再复制链接；v2rayN 保持默认格式。无需第三方订阅转换服务，详见 [周期任务与订阅格式](docs/SCHEDULE_AND_CLASH.md)。

## 更新

在网页“设置 → 版本与更新”检查并安装正式版，或执行：

```sh
vgatebox check-update
vgatebox update
vgatebox update-status
```

更新会短暂中断连接，保留已有管理地址、密码、订阅和出口设置。安装包经过 SHA256 及成员清单校验，更新前保存本项目备份；失败时尝试恢复原部署。备份位于 `/var/backups/vgatebox/`，重装系统前请另存。详见 [安装与更新](docs/INSTALL_UPDATE.md) 和 [备份与恢复](docs/BACKUP_REINSTALL.md)。

## 卸载

```sh
vgatebox uninstall
```

默认保留备份。需要同时删除备份和可确认归属的旧版文件时，先预览再执行：

```sh
vgatebox uninstall --dry-run --purge --legacy
vgatebox uninstall --purge --legacy
```

卸载需确认，不移除其他程序或共享系统依赖。卸载细节见 [卸载说明](docs/UNINSTALL.md)。

## 使用须知

- VPN Gate 节点由志愿者提供，可用数量和国家随时变化；候选数量不等于可用出口数。
- VPN Gate 或自定义代理故障时，对应连接阻断，不会自动改用 VPS 本机直出。
- 国家与住宅类型分别判断。`unknown` 表示类型未知，不能视为已验证住宅 IP；手填国家、城市和备注不是检测证据。
- VPS 转发的订阅和 SOCKS5 需要 VPS 持续运行；原始 SSTP 导出需消费端自行支持相应协议。
- 管理地址、订阅和备份含私密信息，请妥善保存。公网管理建议使用 HTTPS 反向代理或 SSH 转发。
- 当前代理链路面向 TCP / IPv4；内置 Xray 只支持 VLESS。VLESS 覆盖 TCP/RAW、XHTTP、gRPC、WebSocket、HTTPUpgrade、mKCP，以及 none/TLS/REALITY 的有效组合；自动新建入口默认为 VLESS + VLESS Encryption + RAW/TCP + REALITY + xtls-rprx-vision，REALITY 目标默认 `www.tesla.com`。VLESS Encryption 支持 X25519 或 ML-KEM-768。Hysteria2 已移除，升级时旧 Hysteria2 入口会被过滤。UDP 在 IPv4 下全链路启用（v2rayN 需在客户端启用 UDP）；Mihomo 无法表示 VLESS + mKCP，VLESS + mKCP 仍进入 v2rayN 订阅。第三方 SOCKS5 的 UDP 能力取决于上游。

## 文档

[首次使用](docs/FIRST_USE.md) · [第三方 SOCKS5](docs/THIRD_PARTY_SOCKS.md) · [代理资源](docs/PROXY_RESOURCES.md) · [原始 SSTP](docs/SSTP_USAGE.md) · [安装更新](docs/INSTALL_UPDATE.md) · [开发与测试](docs/TESTING.md) · [前端](docs/FRONTEND.md)

核心工具采用 [GPL-3.0-only](LICENSE)。依赖许可、上游来源及复用边界见 [THIRD_PARTY.md](THIRD_PARTY.md)。
