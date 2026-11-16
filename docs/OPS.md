# 运维：安装 / 更新 / 备份 / 卸载 / 兼容

统一安装命令见 [README](../README.md#安装)，自动选择 APT 或 DNF。要求 Linux x86_64、root、systemd、TUN / network namespace。

| 系统 | 状态 |
| --- | --- |
| Debian 12 / 13 | 自动新装、更新、卸载通过 |
| Ubuntu 22.04 / 24.04 | 自动新装、更新、卸载通过 |
| CentOS Stream 9 / 10 | 保留安装支持，不纳入 CI 自动测试 |

ARM64、CentOS 7/8、OpenVZ、非 systemd 不在支持范围。防火墙需放行管理端口（默认 8899）及实际接入端口。

## 容量与资源

- `maximumSlots = 16384` 是每个实例 `/16` 地址空间的 `/30` 链路上限，**不是**单机实际能承载的出口数。
- 启动日志的“容量预检”按 `RLIMIT_NOFILE`、`MemAvailable` 和端口区间给出建议值；它只提示，不修改 `-max`，也不阻止创建。
- 估算口径：每个 Slot 预留 64 个 fd（另留 256 个给进程/诊断），约 32 MiB 可用内存；随机端口默认 `20000-60000`，设置 `FANOUT_SLOT_PORT_BASE` 后为基址到 `65535`。
- 实际容量还受 OpenVPN 进程数、netns/veth、内核内存、iptables 规则和上游节点质量影响。首次部署建议先用较小目标实测 RSS、fd 和连接稳定性，再逐步提高。
- 实测参考（在 VPS 上执行，替换 `<slot>`）：

  ```sh
  ip netns list
  ps -eo pid,rss,cmd | grep '[o]penvpn'
  ls /proc/<openvpn-pid>/fd | wc -l
  ss -lnt | grep -E ':(20000|30000|17928)'
  ```

- 备份目录权限为 `0700`、归档为 `0600`；`tests/test_installer.py` 已断言该权限。备份仍含凭据，请限制访问，不要上传到不可信存储。
- 清理/备份会带上 `state/` 下的 SQLite `-wal`/`-shm` 与凭据；备份目录含私密信息，请限制访问权限，不要上传到不可信存储。

## 更新

网页“设置 → 版本与更新”，或：

```sh
vgatebox check-update
vgatebox update
vgatebox update-status
```

更新短暂中断连接，保留管理地址、密码、订阅和出口设置。更新前备份本项目，失败尝试恢复。

## 备份

备份位于 `/var/backups/vgatebox/`，仅覆盖本项目受管文件（程序、`/etc/vgatebox`、私有状态、服务文件），不是整机镜像。重装前另存到本机。

## 卸载

```sh
vgatebox uninstall
vgatebox uninstall --dry-run --purge --legacy
vgatebox uninstall --purge --legacy
```

默认保留备份。`--purge` 删备份，`--legacy` 额外清理旧版 fanout。需输入 `yes` 确认，不删其他程序或共享依赖。
