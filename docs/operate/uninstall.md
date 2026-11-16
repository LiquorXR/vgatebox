# 卸载

卸载会停止本项目服务、清理本项目创建的 network namespace / veth / iptables 规则，并删除受管文件。默认保留备份；不会删除其他程序、共享系统依赖或外部面板数据。

## 命令

```sh
vgatebox uninstall
```

先预览再执行：

```sh
vgatebox uninstall --dry-run
vgatebox uninstall --dry-run --purge --legacy
```

确认后：

```sh
vgatebox uninstall --purge --legacy
```

| 参数 | 作用 |
| --- | --- |
| `--dry-run` | 只读预览：列出将删除的本项目路径，不停止或删除 |
| `--purge` | 同时删除 `/var/backups/vgatebox`，不可恢复 |
| `--legacy` | 额外清理可确认归属的旧 fanout 安装 |
| `--yes` | 非交互确认；自动化使用。没有该参数时要求在终端输入 `yes` |

脚本会再次确认；输入不是 `yes` 时取消。

## 会删除什么

标准安装：

```text
/etc/systemd/system/vgatebox-fanout.service
/etc/systemd/system/vgatebox-pool.service
/etc/systemd/system/vgatebox-pool.timer
/etc/default/vgatebox-fanout
/usr/local/bin/vgatebox
/usr/local/bin/vgatebox-fanout
/etc/vgatebox
/var/lib/vgatebox
/usr/local/lib/vgatebox
/var/lib/vgatebox-deploy
```

`--purge` 额外删除：

```text
/var/backups/vgatebox
```

`--legacy` 且归属检查通过时，额外清理：

```text
/etc/systemd/system/fanout.service
/usr/local/bin/fanout
/var/lib/fanout
/usr/local/bin/f（只有标准菜单脚本才删）
```

## 不会删除什么

- 3x-ui、xray-cf-lite 等外部面板及其数据。
- 共享系统依赖（OpenVPN、iptables、Python、curl、sstp-client 等）。
- 全局 IP 转发设置。
- 系统运行日志、部署协调锁（由系统管理）。
- `/etc/netns` 共享目录（可能包含其他程序的命名空间）。

防火墙规则、服务商安全组、DNS 记录和反向代理配置需要你自行清理。

## 安全边界

卸载器在删除前会核对归属，遇到以下情况会停止并保留现场：

- 服务单元被自定义，或来自其他安装位置。
- 服务有额外 drop-in 覆盖配置。
- 受管目录是挂载点，或包含挂载点/符号链接。
- 存在旧 fanout 但缺少标准安装归属证据。
- 有未完成的安装/更新事务，或在线更新任务仍在运行。
- 隧道仍有进程，或网络资源清理未完成。

这类情况下不要强行删除；先停止/迁移相关服务，或按提示处理后重试。

## 卸载前

1. 保存需要的订阅地址、客户端配置、出口凭据和备份。
2. 确认没有其他程序依赖 vgatebox 的端口或 network namespace。
3. 如果需要保留数据，先备份 `/var/lib/vgatebox`、`/etc/vgatebox` 和 `/var/backups/vgatebox`。
4. 在维护窗口执行；卸载会中断所有连接。

## 卸载后

- 管理页、订阅和所有出口立即失效。
- 已分发的客户端配置需要删除；如果重装，使用新的管理地址、密码和订阅地址。
- 备份若保留，仍可用 `tar` 查看；不要上传到不可信存储。
- 需要彻底清理时，再按需删除防火墙规则、反向代理配置和 DNS 记录。

## 相关

- [安装](install.md)
- [更新](update.md)
- [备份与恢复](backup-restore.md)
- [状态与文件](../reference/state-and-files.md)
- [CLI 参考](../reference/cli.md)
