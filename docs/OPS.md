# 运维：安装 / 更新 / 备份 / 卸载 / 兼容

统一安装命令见 [README](../README.md#安装)，自动选择 APT 或 DNF。要求 Linux x86_64、root、systemd、TUN / network namespace。

| 系统 | 状态 |
| --- | --- |
| Debian 12 / 13 | 自动新装、更新、卸载通过 |
| Ubuntu 22.04 / 24.04 | 自动新装、更新、卸载通过 |
| CentOS Stream 9 / 10 | 保留安装支持，不纳入 CI 自动测试 |

ARM64、CentOS 7/8、OpenVZ、非 systemd 不在支持范围。防火墙需放行管理端口（默认 8899）及实际接入端口。

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
