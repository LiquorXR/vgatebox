# 安装

vgatebox 以完整包安装到系统目录，由 systemd 管理服务和定时器。安装器会在一个事务里完成依赖、文件、配置、服务启动和健康检查；失败时尝试恢复原部署。

## 系统要求

| 项目 | 要求 |
| --- | --- |
| 架构 | Linux x86_64（amd64） |
| 权限 | root |
| init | systemd |
| 网络 | 可用的 TUN / network namespace；能创建 veth、iptables 规则 |
| 系统 | Debian 12/13、Ubuntu 22.04/24.04、CentOS Stream 9/10 |
| Python | 3.9+（安装器会检查） |
| 磁盘 | 至少数百 MB；完整包、Xray、日志和备份会占用额外空间 |

不支持 ARM64、CentOS 7/8（含 Stream 8）、OpenVZ 等受限虚拟化、非 systemd 系统。CentOS Stream 保留安装支持，但不在自动安装/更新/卸载测试矩阵中，独立 VPS 验收尚未完成。

## 在线安装

以 root 执行。以下命令会自动选择 APT 或 DNF，安装最新正式版：

```sh
if command -v apt-get >/dev/null; then apt-get update && apt-get install -y ca-certificates curl; else dnf -y install ca-certificates && { command -v curl >/dev/null || dnf -y install curl; }; fi &&
curl -fsSL https://github.com/LiquorXR/vgatebox/releases/latest/download/install.sh -o /tmp/vgatebox-install.sh && bash /tmp/vgatebox-install.sh && vgatebox access
```

需要固定版本时：

```sh
bash /tmp/vgatebox-install.sh --version vX.Y.Z
```

## 离线安装

完整包和同目录的 `checksums.txt` 就绪后：

```sh
bash vps/install.sh --mode vps --package /path/vgatebox-linux-amd64.tar.gz
```

安装器只接受 `vgatebox-linux-amd64.tar.gz`，并校验：

- `checksums.txt` 中的 SHA256；
- 包内 `package-sha256.json` 的成员清单；
- `VERSION` 与 `BUILD.json` 的版本一致；
- 包内路径无绝对路径、`..`、链接或重复成员。

包的制作和发布流程见 [发布](../develop/release.md)。

## 安装器做了什么

1. 检查 root、Linux x86_64、systemd、`flock` 和系统依赖。
2. 安装依赖：Debian/Ubuntu 使用 APT；CentOS 使用 DNF，并在缺少 `sstpc` 时从固定版本（`sstp-client 1.0.20`）源码编译本机 RPM。
3. 获取部署锁，恢复未完成的事务（如果有）。
4. 下载/读取完整包，校验并解包。
5. 停止旧服务，备份现有受管文件。
6. 安装程序到 `/usr/local/lib/vgatebox`、`/usr/local/bin`，Xray 到 `/var/lib/vgatebox/fanout/bin/xray`。
7. 写入默认配置（仅当文件不存在）：`/etc/vgatebox/pool.json`、`/etc/vgatebox/intelligence.env`、`/etc/default/vgatebox-fanout`、`settings.json`。
8. 安装并启动 `vgatebox-fanout.service`，启用 `vgatebox-pool.timer`。
9. 首次安装且工作目录为空时，登录后开启「VPS 本机网络」。
10. 健康检查：通过登录态读取版本和 `/api/exits`，确认后端是 native 且面板无错误。

已有安装会保留管理地址、密码、订阅、出口和候选池；不会被默认配置覆盖。

## 安装后验证

```sh
vgatebox access          # 管理地址和密码
vgatebox status          # 服务和定时器状态
systemctl status vgatebox-fanout --no-pager
journalctl -u vgatebox-fanout -n 100 --no-pager
```

浏览器打开 `vgatebox access` 显示的地址，登录后应看到「总览」。也可以在本机请求免登录探活：

```sh
curl -fsS http://127.0.0.1:8899/healthz
```

如果管理页使用了随机访问路径或非默认端口，把地址替换为实际值。`/healthz` 只返回版本、uptime、Slot 计数、候选池龄和面板状态，不含敏感字段。

## 防火墙

需要放行：

- 管理端口：默认 `8899`；如果改了端口，以「设置」页为准。
- 接入端口：VPS SOCKS5 的外部端口、手动 VLESS 入站的端口、实际使用的节点端口。
- 本机网络和自动 VLESS 入口的端口：在「接入 → 高级接入」或出口详情里查看。
- 需要 UDP 时，同时放行对应 UDP 端口。

系统防火墙（`ufw`、`firewalld`、`iptables`）和服务商安全组都要检查。只放行 8899 不足以让客户端连接代理。

## 目录与权限

| 路径 | 用途 | 权限 |
| --- | --- | --- |
| `/usr/local/lib/vgatebox` | 程序、部署脚本、Python 工具箱 | 受管 |
| `/usr/local/bin/vgatebox` | 管理 CLI | 0755 |
| `/usr/local/bin/vgatebox-fanout` | Go 管理进程 | 0755 |
| `/etc/vgatebox` | `pool.json`、`intelligence.env` | 0700 / 0600 |
| `/etc/default/vgatebox-fanout` | 环境变量 | 0600 |
| `/var/lib/vgatebox/pool` | 候选池 | 0700 |
| `/var/lib/vgatebox/fanout` | 工作目录、状态、缓存、运行态 | 0700 |
| `/var/backups/vgatebox` | 安装/更新备份 | 0700，归档 0600 |
| `/var/lib/vgatebox-deploy` | 部署事务与更新状态 | 0700 |

完整清单见 [状态与文件](../reference/state-and-files.md)。

## 反向代理

如果通过 HTTPS 反向代理访问管理页：

- 设置 `FANOUT_PUBLIC_URL=https://panel.example.com`（只填外部 origin，不要带路径）。
- 代理把 `X-Forwarded-Proto: https` 传给管理服务，并确保代理地址被识别为可信（本机/私有网段或 `FANOUT_TRUSTED_PROXIES`）。
- 代理读超时至少 120 秒，避免入口重载时出现 502。
- 代理只代理管理页面时，节点链接优先使用 `FANOUT_PUBLIC_IP` 或自动探测到的公网 IPv4。

详见 [安全](security.md)。

## 安装失败

安装器会输出阶段信息，失败时尝试恢复原部署；备份位于 `/var/backups/vgatebox/`。常见原因：

| 现象 | 处理 |
| --- | --- |
| 提示不支持的系统/架构 | 换支持的 Debian/Ubuntu/CentOS Stream x86_64 VPS |
| 依赖安装失败 | 检查系统源、DNS、磁盘空间；修复后重跑安装入口 |
| 包校验失败 | 重新下载完整包和 `checksums.txt`；不要手工改包 |
| 健康检查失败 | `systemctl status vgatebox-fanout`、`journalctl -u vgatebox-fanout`；查看是否端口被占用 |
| 检测到外部面板配置 | 先停止并迁移外部面板；vgatebox 不会删除外部数据 |
| 安装事务未完成 | 重新运行安装入口；安装器会先尝试恢复 |

更完整的排查见 [故障排查](troubleshooting.md)。

## 相关

- [更新](update.md)
- [卸载](uninstall.md)
- [安全](security.md)
- [容量与资源](capacity.md)
- [快速开始](../start/quickstart.md)
