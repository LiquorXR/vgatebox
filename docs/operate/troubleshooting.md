# 故障排查

先做四件事，绝大多数问题能定位到方向：

```sh
vgatebox access                 # 管理地址和密码
vgatebox status                 # 服务和定时器状态
journalctl -u vgatebox-fanout -n 200 --no-pager
curl -fsS http://127.0.0.1:8899/healthz
```

如果管理页使用了随机路径或非默认端口，把 `8899` 和路径替换成实际值。`/healthz` 只返回非敏感字段，适合本机探活。

## 症状速查

| 症状 | 最可能的原因 | 先做什么 |
| --- | --- | --- |
| 管理页打不开 | 服务未启动、端口/路径不对、防火墙、只监听本机 | `vgatebox status`；`ss -lntp`；检查防火墙 |
| 登录失败/被锁 | 密码错、连续失败锁定、Cookie/HTTPS 问题 | 等 2 分钟；`vgatebox access`；用 HTTPS/SSH 转发 |
| 反向代理 502 | 代理超时、可信代理/转发头未配置 | 读超时 ≥120s；透传 `X-Forwarded-Proto` |
| 订阅 404 | token 错、没有已就绪出口、范围为空 | 复制完整地址；先启用本机网络；看总览计数 |
| 订阅能拉但客户端没节点 | 格式选错、Clash 跳过 mKCP、客户端未更新 | 换格式；更新订阅；看订阅范围 |
| 客户端连上但打不开网页 | 接入端口未放行、出口未就绪、上游故障 | 先试本机网络；检查接入端口和出口状态 |
| 出口一直“等待候选” | 候选为空、策略过严、候选被占用 | 刷新候选；放宽国家/允许 `unknown` |
| 出口“已阻断” | 证据过期、出口 IP 变化、策略不匹配、绑定失败 | 看详情原因；重新检测/换 IP/改策略 |
| 出口“失败” | OpenVPN 建连失败、端口/fd 不足、上游不可达 | 看日志；降低并发/数量；换地区 |
| 出口频繁重连 | 上游抖动、延迟超限、节点被限 | 换国家/策略；降低目标数量；看 `slot.phase` |
| 刷新候选失败 | 来源不可达、镜像关闭/过期、DNS | `vgatebox refresh`；检查来源设置和网络 |
| 检测大量 deferred | 并发被正式出口占用 | 等待；降低目标数量；不要强杀隧道 |
| 更新失败/中断 | 网络、包校验、健康检查 | `vgatebox update-status`；重跑安装入口 |
| 内存/fd/端口告警 | 出口太多、并发太高 | 降低目标/并发；看 [容量](capacity.md) |
| SSTP 检测失败 | 缺 `sstpc`/`pppd`、端点不可达、证书拒绝 | 看依赖错误；换节点；不要关闭证书校验 |
| 第三方代理检测失败 | 地址/端口/账号错、上游不可达 | 核对供应商信息；重试；看检测代码 |

## 管理页与登录

### 打不开

1. 确认服务：

   ```sh
   systemctl status vgatebox-fanout --no-pager
   journalctl -u vgatebox-fanout -n 100 --no-pager
   ```

2. 确认监听：

   ```sh
   ss -lntp | grep vgatebox
   ```

   如果 `ListenAddr` 是 `127.0.0.1`，只能本机访问；用 SSH 端口转发或改成 `0.0.0.0`（注意安全）。

3. 确认地址：

   ```sh
   vgatebox access
   ```

   地址包含随机访问路径，不能只访问 `http://IP:8899/`。

4. 检查防火墙：

   ```sh
   iptables -S | head -50
   # 或
   ufw status
   firewall-cmd --list-all
   ```

   系统防火墙和服务商安全组都要放行。

### 登录失败

- 密码以 `vgatebox access` 为准；不要从旧截图复制。
- 同一来源连续失败 8 次会锁定 2 分钟；等待后重试。
- 通过 HTTPS 反向代理访问时，确认 `X-Forwarded-Proto: https` 和可信代理配置；否则 Cookie 可能没有 `Secure`，或浏览器不发送。
- 如果浏览器插件/跨站页面发起写请求，`Sec-Fetch-Site: cross-site` 会被拒绝；用管理页本身操作。
- 忘记密码只能从 `state/password` 读取；没有“邮件重置”。

### 反向代理 502

- 代理读超时至少 120 秒；入口重载/保存第三方代理会触发 Xray 重载。
- 确认代理地址是本机/私有网段，或在 `FANOUT_TRUSTED_PROXIES` 中。
- 确认代理透传 `X-Forwarded-Proto`、`X-Forwarded-Host`。
- 路径前缀要配置为管理访问路径，不要写进 `FANOUT_PUBLIC_URL`。
- 看 `journalctl -u vgatebox-fanout` 是否有 5xx/panic 日志。

## 订阅

### 404

- token 错误：完整复制订阅地址，不要漏字符；必要时「重置口令」后重新复制。
- 没有已就绪出口：先在「总览」启用本机网络，或等待 VPN Gate 出口「已就绪」。
- 订阅范围为空：检查「VPS 本机网络」「VPN Gate」「自定义代理」是否至少选中一项。
- 使用 `slots`/`proxies` 参数时，确认编号存在；非法值返回 400。

### 能拉取但客户端没有节点

- v2rayN 用 `v2rayN / 通用节点列表`；Clash 用 `Clash Verge / Rev（Mihomo）`。
- Clash 会跳过 VLESS + mKCP；这类入口只在 v2rayN 格式里。
- 客户端需要主动更新订阅；新增出口后不会自动出现。
- 第三方代理需要同时开启「启用」和「加入订阅」。
- VPN Gate 只输出 `up` 出口；`degraded` 也不进订阅。

### 订阅泄露

- 「订阅」页点「重置口令」，旧地址立即失效；然后在所有客户端重新导入。
- 同时检查管理密码和公网 SOCKS5 凭据是否泄露。

## 客户端连不上

1. 先用「VPS 本机网络」验证客户端、端口和防火墙；如果本机网络也不通，问题在接入端口或客户端配置。
2. 确认选中的节点是「已就绪」，不是「等待候选/已阻断/已停止」。
3. 确认防火墙放行的是**接入端口**，不是管理端口。
4. 在 VPS 上检查监听：

   ```sh
   ss -lntup | grep -E ':(8899|30000|20000|17928)'
   ```

5. 检查出口：

   - 出口详情里的实际出口 IP 是否有效；
   - 是否被策略阻断；
   - 上游是否在线。

6. 需要 UDP 时，确认客户端启用了 UDP，防火墙放行 UDP；第三方 SOCKS5 的 UDP 取决于上游。

## 出口状态

### 等待候选

- 候选为 0：刷新来源；看来源设置和镜像后备。
- 候选不满足策略：国家、IP 类型、ISP/ASN、延迟限制可能过严；先放宽国家或允许 `unknown`。
- 候选被其他出口占用：同一节点不会同时给多个出口；减少数量或换地区。
- 上游不可达：VPN Gate 节点临时下线；等待或刷新。

### 已阻断

出口详情会显示原因，常见：

- 画像过期/缺失：重新检测或换 IP；策略要求 ISP/ASN 时必须有完整画像。
- 实际出口 IP 变化：隧道可能断开并退回公网；系统会阻断并重连。
- 国家/类型不匹配：调整策略或换节点。
- 绑定失败：检查入站和出口的绑定关系；重启出口；看日志。

### 失败

- OpenVPN 建连失败：节点不可用、协议被干扰、端口被占用。
- fd/端口不足：降低目标数量或提高限制；看 [容量](capacity.md)。
- namespace/veth 创建失败：检查内核/权限/残留资源。
- 查看 `journalctl -u vgatebox-fanout` 和 `run/*.log`。

### 频繁重连

- 10 分钟内多次重连会降频；这是保护，不是故障。
- 延迟持续超限会触发换节点；如果所有节点都慢，换地区或降低目标。
- 上游节点质量差时，减少数量、换国家，或使用第三方 SOCKS5。

## 节点池

- **刷新失败**：检查 VPS 到官方来源的 HTTPS；看 `vgatebox refresh` 的 stderr；确认镜像后备是否开启。
- **刷新成功但候选 0**：CSV 格式、解析错误、筛选条件；看 `node_pool.json` 的 `source.errors`。
- **检测 deferred**：并发被正式出口占用；等待或降低目标数量；不要强杀隧道。
- **结果过期**：固定 30 分钟 TTL；重新检测或等下一轮自动检测。
- **镜像可疑**：在「来源设置」关闭镜像；只使用官方来源，失败时保留旧候选。

## 更新与卸载

- 更新失败：`vgatebox update-status`；看 `/var/backups/vgatebox/update-<id>/install.log`。
- 更新中断：重新运行 `vgatebox update`；安装器会先恢复未完成事务。
- 卸载被拒绝：服务单元被自定义、路径含挂载点/符号链接、存在未完成事务或运行中的更新；按提示保留现场，不要强删。
- 卸载后无法重装：检查 `/var/lib/vgatebox-deploy` 和锁文件；重新运行安装入口。

## 收集信息

提交问题时，提供：

```sh
vgatebox version
vgatebox status
vgatebox update-status
systemctl status vgatebox-fanout --no-pager
journalctl -u vgatebox-fanout -n 300 --no-pager
ip netns list
ss -lntup
```

并说明：系统版本、VPS 规格、出口数量、是否使用反向代理、客户端类型、复现步骤。

**不要**贴出：管理密码、订阅 token/完整订阅地址、二维码、第三方代理账号密码、备份文件、`state/password`、`state/basepath`。

日志里的请求 ID 可用于对照一次请求；日志不记录 query/body/请求头。

## 相关

- [安装](install.md)
- [更新](update.md)
- [安全](security.md)
- [容量与资源](capacity.md)
- [出口](../use/exits.md)
- [节点池](../use/nodes.md)
- [订阅](../use/subscription.md)
- [状态与文件](../reference/state-and-files.md)
