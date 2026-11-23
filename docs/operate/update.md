# 更新

vgatebox 的更新是完整包替换：下载、校验、备份、停止服务、安装、健康检查、启动；失败时尝试恢复原部署。管理地址、密码、订阅、出口和候选池会保留。

## 检查与安装

### 网页

「设置 → 版本与更新」：

1. 点「检查更新」，显示当前版本、最新正式版和发布说明。
2. 有可用版本时点「安装更新」。
3. 更新期间服务会短暂中断；页面会显示阶段和结果。
4. 完成后重新登录/刷新页面；客户端连接会短暂中断，之后恢复。

在线更新只适用于系统安装实例：

- 工作目录必须是 `/var/lib/vgatebox/fanout`；
- 节点后端必须是内置 Xray（native）；
- 系统必须有 `/usr/local/bin/vgatebox`、`/usr/local/lib/vgatebox/deploy/installer.py` 和 `systemd-run`。

自定义工作目录或非系统安装请使用对应的安装入口。

### 命令行

```sh
vgatebox check-update     # 查当前版本和最新正式版
vgatebox update           # 更新到最新正式版
vgatebox update --version vX.Y.Z
vgatebox update-status    # 查看最近一次更新任务
```

`vgatebox update` 会调用安装器，等价于重新运行安装入口。离线包：

```sh
vgatebox install --package /path/vgatebox-linux-amd64.tar.gz
```

包旁必须有 `checksums.txt`。

## 更新过程

| 阶段 | 做什么 |
| --- | --- |
| `preparing` | 检查版本、准备任务 |
| `dependencies` | 检查/安装系统依赖 |
| `downloading` | 下载完整包和 `checksums.txt` |
| `verifying` | 校验 SHA256、成员清单、版本和源码信息 |
| `pool` | 校验本地候选配置；不等待 VPN Gate 来源 |
| `stopping` | 停止 fanout 服务、候选定时器 |
| `backup` | 备份受管文件到 `/var/backups/vgatebox/install-<job-id>/` |
| `checking` | 用新版本只读检查已有状态 |
| `installing` | 替换程序文件、配置和 systemd 单元 |
| `health` | 启动服务，登录并检查版本和出口视图 |
| `complete` | 提交事务，清理部署日志 |
| `rollback` | 失败时恢复备份和服务状态 |
| `recovery_required` | 自动恢复未完成，需要重新运行安装入口 |

网页更新由 `systemd-run` 启动独立任务，日志在：

```text
/var/backups/vgatebox/update-<id>/install.log
```

任务中断后，`vgatebox update-status` 会显示 `interrupted`；重新运行安装入口会先尝试恢复。

## 备份与恢复

- 每次更新前都会保存本项目受管文件的备份，路径 `/var/backups/vgatebox/install-<job-id>/project.tar.gz`。
- 备份目录权限 `0700`，归档 `0600`；仍含凭据和私钥，不要上传到不可信存储。
- 更新失败时安装器自动恢复文件和服务；恢复成功后原部署继续运行。
- 自动恢复失败时显示 `recovery_required`；不要手工改状态文件，重新运行安装入口或按 [备份与恢复](backup-restore.md) 处理。
- 备份不是整机镜像，只覆盖本项目受管文件。重装系统前请另存到本机。

## 更新会保留什么

- 管理地址（随机访问路径）、密码、订阅 token。
- `/etc/vgatebox` 下的来源配置和画像配置。
- 出口意图、策略、凭据、候选池。
- 已创建的入站和客户端链接。
- 备份目录（默认保留）。

## 更新会中断什么

- 更新期间管理页和订阅短暂不可用。
- 所有隧道会短暂断开；客户端需要等待恢复，通常不需要重新导入订阅。
- 入口重载可能让反向代理出现短暂 502；把代理读超时设为至少 120 秒。

## 版本与兼容

- 网页更新只使用 GitHub Releases 的正式版，不会使用 draft 或 prerelease；命令行显式指定版本时也只应使用正式版。
- 完整包必须包含与版本一致的 `BUILD.json` 和 `VERSION`；来源不干净的包会被拒绝。
- 从源码构建的版本用于开发/测试，不作为在线更新源。
- 旧版本的配置迁移由安装器和 `-check-state` 只读检查处理；不确定时先备份。

## 更新失败

| 现象 | 处理 |
| --- | --- |
| 检查更新失败 | VPS 能否访问 GitHub API；稍后重试或用离线包 |
| “当前架构暂无可用的正式完整包” | 该版本没有对应架构的包；换版本或使用源码构建 |
| “已有更新任务运行” | 等当前任务结束；用 `vgatebox update-status` 查看 |
| 更新中断 | 重新运行 `vgatebox update`；安装器会先恢复 |
| 健康检查失败 | 看 `/var/backups/vgatebox/update-<id>/install.log` 和 `journalctl -u vgatebox-fanout` |
| 更新后管理页打不开 | 检查端口/访问路径是否保留；`vgatebox access` 重新读取 |
| 反向代理 502 | 增大代理读超时；确认代理地址可信并透传 `X-Forwarded-Proto` |

更多排查见 [故障排查](troubleshooting.md)。

## 相关

- [安装](install.md)
- [备份与恢复](backup-restore.md)
- [卸载](uninstall.md)
- [安全](security.md)
- [发布流程](../develop/release.md)
