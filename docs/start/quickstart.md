# 快速开始：从一台空 VPS 到客户端能上网

读完后，你会得到一个可登录的管理页、一条订阅链接，并能在 v2rayN 或 Clash Verge 里通过 vgatebox 上网。全程大约 10–15 分钟，不含等待 VPN Gate 候选的时间。

## 前置条件

- 一台 Linux x86_64 VPS，root 权限，systemd，可用的 TUN / network namespace。
- 支持的系统：Debian 12/13、Ubuntu 22.04/24.04、CentOS Stream 9/10。
- 放行管理端口（默认 `8899`）和后续实际使用的接入端口；系统防火墙与服务商防火墙都要检查。
- 一个客户端：v2rayN（Windows/Android 等）或 Clash Verge / Rev（Mihomo 内核）。

ARM64、CentOS 7/8、OpenVZ、非 systemd 系统不在支持范围。完整矩阵见 [边界与兼容](../reference/limits.md)。

## 1. 安装

以 root 执行。以下命令会自动选择 APT 或 DNF，安装最新正式版：

```sh
if command -v apt-get >/dev/null; then apt-get update && apt-get install -y ca-certificates curl; else dnf -y install ca-certificates && { command -v curl >/dev/null || dnf -y install curl; }; fi &&
curl -fsSL https://github.com/LiquorXR/vgatebox/releases/latest/download/install.sh -o /tmp/vgatebox-install.sh && bash /tmp/vgatebox-install.sh && vgatebox access
```

需要固定版本时：

```sh
bash /tmp/vgatebox-install.sh --version vX.Y.Z
```

CentOS 首次安装会从固定版本源码编译 SSTP 客户端，耗时和内存需求高于 Debian/Ubuntu。

## 2. 取得管理地址并登录

安装结束时会打印管理地址和密码；之后随时可以再查：

```sh
vgatebox access
```

在浏览器打开类似下面的地址（`<访问路径>` 是安装时随机生成的，不是固定的 `/`）：

```text
http://<VPS公网IP>:8899/<访问路径>/
```

输入 `vgatebox access` 显示的密码登录。登录态是 Cookie，默认 12 小时；改密码后其他浏览器需要重新登录。

> 管理页走公网 HTTP 时，建议用 HTTPS 反向代理或 SSH 端口转发；具体做法见 [安全](../operate/security.md)。

## 3. 先用「VPS 本机网络」验证客户端

首次安装默认开启「VPS 本机网络」。它使用 VPS 公网 IP 直出，不依赖 VPN Gate，适合先确认客户端、端口和防火墙都通。

1. 打开「总览 → VPS 本机网络」或「出口 → 本机网络」，确认入口已启用；如果关闭了，点「启用」。
2. 打开「订阅」，保持默认格式 `v2rayN / 通用节点列表`，点「复制订阅」。
3. 在客户端新建订阅并更新，选择 `VPS 本机网络`。
4. 打开一个网页验证；也可以在客户端查看当前出口 IP，应等于 VPS 公网 IP。

如果这一步不通，先不要继续创建 VPN Gate 出口；按文末「失败处理」检查接入端口和防火墙。

## 4. 创建 VPN Gate 出口（可选）

1. 打开「出口」，点「调整数量」。
2. 首次没有保存数量、也没有旧出口时，取得候选后默认创建 **6** 个出口；已有数量（包括 0）会保留。
3. 等待出口从「等待候选 / 连接中」变为「已就绪」。候选不足、上游不可达或策略过严时可能长时间等待，这是正常状态。
4. 每个出口会自动创建一个 VLESS 入站，并绑定到该出口；普通使用不需要手动新建入站。

出口数量是“想要几个”，不等于当前可用数。已就绪数量、等待数量和失败原因在「出口」页和「总览」页都能看到。

## 5. 导入订阅并使用

1. 打开「订阅」。
2. 选择客户端格式：
   - v2rayN 保持 `v2rayN / 通用节点列表`。
   - Clash Verge / Rev 选择 `Clash Verge / Rev（Mihomo）`。
3. 选择订阅范围：
   - 「VPS 本机网络」独立开关。
   - 「VPN Gate」默认全选，只输出已就绪出口中已启用的入站。
   - 「自定义代理」需要第三方代理本身开启「加入订阅」。
4. 点「复制订阅链接」，在客户端新建订阅或远程配置并更新。
5. 选择 `VPS 本机网络` 或任一「已就绪」的 VPN Gate 出口，启用系统代理/规则模式，访问网页验证。
6. 订阅地址里的 token 等同密码；不要发到公开群组或截图里。需要让旧地址立即失效时，在「订阅」页点「重置口令」。

客户端只会看到入口，不会看到 VPN Gate 的 OpenVPN 凭据，也不会看到第三方 SOCKS5 的账号密码。

## 6. 验证与后续

- 出口就绪后，客户端需要**主动更新订阅**才能看到新增节点。
- 换国家、换 IP、加第三方 SOCKS5、设置周期刷新，见 [出口](../use/exits.md)、[接入](../use/access.md)、[节点池](../use/nodes.md)。
- 需要给其他电脑或程序提供 VPS SOCKS5，见 [接入](../use/access.md)。
- 需要 SSTP 客户端直接连接，见 [原始 SSTP](../use/sstp.md)。

## 失败处理

| 现象 | 先检查 | 处理 |
| --- | --- | --- |
| 管理页打不开 | `vgatebox access` 的地址、管理端口、系统防火墙、服务商防火墙 | 放行 8899；确认服务 `systemctl status vgatebox-fanout` |
| 密码不对或忘记 | `vgatebox access` | 重新读取；不要从旧截图复制 |
| 订阅返回 404 | 是否有已就绪出口、是否选了订阅范围、token 是否完整 | 先启用本机网络或等待出口就绪；重置口令后重新复制 |
| 客户端连上但打不开网页 | 实际接入端口是否放行、选中的出口是否「已就绪」、上游是否在线 | 先试本机网络；再检查接入端口和出口状态 |
| 出口一直「等待候选」 | 候选数量、来源刷新状态、国家/IP 类型策略 | 刷新候选；放宽国家或允许 `unknown`；见 [节点池](../use/nodes.md) |
| Clash 导入后少节点 | 是否用了 Clash 格式、是否包含 mKCP 入口 | mKCP 无法被 Mihomo 表示，只保留在 v2rayN 格式；见 [订阅格式](../reference/subscription-format.md) |
| 更新失败 | `vgatebox update-status` | 更新前有备份；按 [更新](../operate/update.md) 恢复或重试 |

更完整的排查路径见 [故障排查](../operate/troubleshooting.md)。
