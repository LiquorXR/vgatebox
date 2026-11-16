# 容量与资源

vgatebox 的出口数量不是只看 `-max`。每条出口都需要文件描述符、内存、network namespace、veth、iptables 规则和一个 OpenVPN 进程；实际能承载多少取决于 VPS 规格和上游质量。

## 上限与建议值

| 指标 | 值 | 说明 |
| --- | --- | --- |
| 实例地址空间上限 | `maximumSlots = 16384` | 每个 Slot 使用一个 `/30` 链路；这是地址空间上限，不是单机建议值 |
| 管理员容量 | `-max` / 设置页 | 启动参数，默认等于实例容量；实际以它为准 |
| 建议 fd 预留 | 256 | 进程、诊断和系统开销 |
| 建议每 Slot fd | 64 | 估算值，不是硬限制 |
| 建议每 Slot 内存 | 32 MiB | 估算值，含 OpenVPN/Xray/状态开销 |
| 随机端口范围 | `20000-60000` | 设置 `FANOUT_SLOT_PORT_BASE` 后为基址到 `65535` |
| 管理端口 | 8899 | 可在设置页修改 |
| 默认检测并发 | 2 | 检测与正式建连共享预算 |

启动日志会输出“容量预检”：

```text
容量预检: 本机建议最多 N 个出口（文件描述符上限；fd=...、内存=... MiB、端口=...）
```

它只提示，不修改 `-max`，也不阻止创建。`-max` 仍是管理员决定；高于建议值时日志会再提示一次。

## 估算口径

建议值取以下三者的最小值：

```text
按 fd：   (RLIMIT_NOFILE - 256) / 64
按内存：  MemAvailable / 32 MiB
按端口：  端口区间大小
```

再与 `-max` 和实例地址空间取小。这个估算偏保守，适合首次部署；实际还要看：

- OpenVPN 进程数和每个进程的 RSS；
- Xray 的入站/出站和连接数；
- netns/veth 数量、内核内存和 conntrack 表；
- iptables 规则数量；
- 上游节点的延迟、丢包和重连频率；
- 检测/健康检查的并发。

## 首次部署建议

1. 先用默认目标 6 个出口，观察 24 小时。
2. 在 VPS 上测量：

   ```sh
   ip netns list | wc -l
   ps -eo pid,rss,cmd | grep '[o]penvpn' | wc -l
   ps -eo rss,cmd | grep '[o]penvpn' | awk '{s+=$1} END {print s/1024 " MiB"}'
   ls /proc/$(pgrep -f 'vgatebox-fanout' | head -1)/fd | wc -l
   ss -lnt | grep -E ':(8899|17928|20000|30000)'
   free -m
   ```

3. 确认 RSS、fd、连接稳定后，再按 6 → 12 → 24 逐步提高，不要一次拉到很高。
4. 如果出口频繁重连或检测大量 `deferred`，先降低目标数量或检测并发，而不是继续加机器资源。

## 文件描述符

- 每条出口需要监听 socket、veth、OpenVPN 管道、日志、SOCKS 连接等；估算按 64 个 fd/Slot。
- systemd 服务没有单独写 `LimitNOFILE`，使用系统默认。可以用 `systemctl show vgatebox-fanout -p LimitNOFILE` 查看。
- 提高限制时同时考虑系统级 `fs.file-max` 和登录 shell 的 `ulimit`；只改 shell 不影响 systemd 服务。
- fd 不足时表现为建连失败、日志报 `too many open files`、出口反复 `failed`。

## 内存

- 32 MiB/Slot 是保守估算；实际 OpenVPN + Xray + Go 进程 + 内核对象可能更高。
- 内存不足时优先降低出口数量、检测并发和候选池规模；不要依赖 swap 跑大量 OpenVPN。
- `MemAvailable` 会随缓存和负载变化；容量预检只取启动时快照。

## 端口

- 内部 SOCKS5 监听在 `127.0.0.1`，默认从 `20000-60000` 随机分配。
- 设置 `FANOUT_SLOT_PORT_BASE=17928` 后，端口从 17928 到 65535；已有出口的端口会尽量保持。
- 自动 VLESS 入站优先使用 `30000 + Slot`，被占用时随机分配。
- 手动入站端口由你指定或随机分配；mKCP 需要 UDP 端口可用。
- 管理端口和接入端口都要在防火墙放行；端口冲突时用 `ss -lntup` 排查。

## 检测与建连并发

- 「节点池 → 检测设置」的并发数同时约束候选检测和正式 OpenVPN 建连。
- 正式出口恢复优先；检测在名额不足时显示 `deferred`，不会抢占正在恢复的出口。
- 调低并发不会强断已有隧道；让进行中的任务完成，新任务按新限制排队。
- 并发越高，CPU、内存、上游连接数和被限流的概率越高；默认 2 适合小 VPS。

## 监控

- `/healthz`：版本、uptime、Slot 总数/up 数、候选池龄、面板状态。
- `/api/metrics`（需登录）：Slot 状态计数、候选池龄、Xray 重启次数、上次检测数、HTTP 请求计数。
- `journalctl -u vgatebox-fanout -f`：结构化日志，含 `slot.phase`、请求 ID、错误。
- `ip netns list`、`iptables -S`、`ss -lntup`：网络资源。

建议抓取间隔 15 秒以上；指标标签只保留固定路由，未知路径记为 `other`，避免基数爆炸。

## 扩容与缩容

- **扩容**：先提高 `-max` 或设置页容量，再逐步提高目标数量；观察 fd/内存/端口。
- **缩容**：降低目标数量会从高编号往下删除；删除单个出口会同时把目标数量减 1。
- **迁移**：换更大的 VPS 时，先备份，再安装并恢复；不要只复制二进制。
- **清理**：删除不用的手动入站、归档入口和公网 SOCKS5，释放端口和 fd。

## 相关

- [安装](install.md)
- [安全](security.md)
- [故障排查](troubleshooting.md)
- [配置参考](../reference/configuration.md)：`FANOUT_SLOT_PORT_BASE`、`-max`
- [状态与文件](../reference/state-and-files.md)
