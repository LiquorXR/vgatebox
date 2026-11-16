# 产品概览

vgatebox 是一个自托管的出口管理器：它把 **VPN Gate 志愿者节点**、**第三方 SOCKS5** 和 **VPS 本机网络**整理成统一的接入入口，再通过一条订阅链接交给 v2rayN、Clash/Mihomo 等客户端使用。

它解决的是“有 VPS，但不想依赖第三方订阅站或外部面板”的场景：候选获取、节点检测、隧道建立、订阅生成、更新和卸载都在自己的机器上完成。

## 适合谁

- 有一台 Linux VPS，能以 root 安装 systemd 服务和 TUN/network namespace。
- 想使用 VPN Gate，但希望自己控制候选来源、检测和出口数量。
- 需要把 VPN Gate、自己的 SOCKS5 和 VPS 本机网络放进同一个客户端订阅。
- 需要网页管理、订阅格式兼容、可更新和可卸载的完整部署。

不适合：没有 root 的共享主机、OpenVZ 等受限虚拟化、非 systemd 系统、ARM 主机，或希望“安装后永久免维护”的场景。详见 [边界与兼容](../reference/limits.md)。

## 它做什么

1. **获取候选**：从 VPN Gate 官方 CSV 获取节点，必要时使用第三方镜像后备；只保存候选，不把候选当可用出口。
2. **检测候选**：按需或周期性地临时连接候选，记录连通性、出口 IP、国家、延迟和 IP 类型证据。
3. **维护出口**：按用户设定的数量和策略创建隔离的 OpenVPN 隧道；每条隧道有独立 network namespace、TUN、SOCKS5 端口和状态机。
4. **提供接入**：内置 Xray 生成 VLESS 入站，把客户端流量转发到出口、第三方 SOCKS5 或本机网络；接入可以自动绑定出口，也可以手动创建。
5. **生成订阅**：把已就绪的接入聚合为 v2rayN（Base64）或 Clash/Mihomo（YAML）订阅，按本机网络、第三方代理、VPN Gate 排序。
6. **后台收敛**：用户只表达“要几个出口、什么策略”；后台持续对账，处理掉线、换节点、删除和重启恢复。

## 组件

| 组件 | 位置 | 职责 |
| --- | --- | --- |
| Python 工具箱 | `src/vpngate_toolbox/` | 获取/解析候选 CSV、构建 `node_pool.json`、查询 IP 画像、周期调度 |
| Go 管理进程 | `vps/fanout/` | 出口生命周期、network namespace、OpenVPN、内置 Xray、HTTP API、订阅、更新 |
| Vue 控制台 | `vps/fanout/frontend/` | 总览、出口、节点池、订阅、接入、设置；构建后由 Go `embed` 进二进制 |
| 安装/卸载器 | `vps/deploy/` | systemd 单元、完整包安装、备份、失败恢复、卸载 |
| 系统定时器 | `vps/deploy/vgatebox-pool.timer` | 周期性触发候选刷新；是否真正刷新由控制台设置决定 |

## 数据流

```text
VPN Gate 官方 CSV / 镜像
        │
        ▼
Python 工具箱 ──► /var/lib/vgatebox/pool/node_pool.json
        │
        ▼
Go 管理进程
  ├─ 候选检测（临时 netns + OpenVPN）──► 证据（时间/配置摘要/有效期）
  ├─ 出口 Slot（netns + tun0 + OpenVPN + 本机 SOCKS5）
  ├─ 内置 Xray
  │    ├─ VLESS 入站（REALITY / TLS / 传输组合）
  │    ├─ 出站：Slot SOCKS5 / 第三方 SOCKS5 / 本机直出
  │    └─ 路由：入站绑定哪个出口，就走哪个出站
  └─ HTTP 控制台 + 订阅 + 健康检查 + Prometheus 指标
        │
        ▼
浏览器 / v2rayN / Clash Verge / Mihomo
```

客户端只连到内置 Xray 的 VLESS 入站；Xray 再按绑定关系把流量送到某条出口。出口故障时，绑定该出口的入站会被阻断，不会自动改走本机网络。

## 关键设计

- **意图与对账**：出口数量、策略、删除目标落盘为 `console.json`；后台按目标收敛，崩溃重启后从磁盘恢复意图，而不是从运行态反推。
- **隔离**：每条 VPN Gate 出口在自己的 network namespace 中运行 OpenVPN，通过 veth 与主机通信；主机侧有 iptables 转发和 kill switch，避免隧道断开后流量退回公网。
- **证据链**：检测结果带 `checked_at`、`expires_at` 和配置摘要；过期或配置变化后自动降级为 `stale`，不会把旧结论当现状。
- **单一入口后端**：节点链接只由内置 Xray 提供；不支持 3x-ui、xray-cf-lite 等外部面板，也不依赖 Cloudflare。
- **订阅即契约**：订阅地址带独立 token，仍在随机访问路径之后；只输出已就绪的 VPN Gate 入口，第三方代理还需要显式开启“加入订阅”。

## 下一步

- 第一次部署：[快速开始](quickstart.md)
- 理解术语和状态：[产品与概念](concepts.md)
- 看支持范围：[边界与兼容](../reference/limits.md)
