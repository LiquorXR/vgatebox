# 安装器

安装器负责把完整包部署到系统目录、管理 systemd 单元、备份/恢复、更新和卸载。它由 shell 入口 `vps/install.sh` 和 Python 事务 `vps/deploy/installer.py` / `uninstall.py` 组成。

## 文件

| 文件 | 作用 |
| --- | --- |
| `vps/install.sh` | 在线/离线安装入口：检查系统、安装依赖、下载/校验、解包、调用 Python 安装器 |
| `vps/deploy/installer.py` | 部署事务：备份、停服、安装、健康检查、回滚、状态 |
| `vps/deploy/uninstall.py` | 受管卸载：归属检查、netns/iptables 清理、文件删除 |
| `vps/deploy/vgatebox` | 系统 CLI 包装脚本 |
| `vps/deploy/vgatebox-fanout.service` | 管理进程单元 |
| `vps/deploy/vgatebox-pool.service` | 候选构建器 oneshot |
| `vps/deploy/vgatebox-pool.timer` | 每 30 分钟触发候选构建器 |
| `scripts/build_vps_package.py` | 构建完整包、校验 Xray、生成清单 |
| `tests/test_installer.py` | 安装/更新/回滚测试 |
| `tests/test_uninstall.py` | 卸载/归属/网络资源测试 |
| `tests/distro_*` | 多发行版特权容器测试 |

## 安装入口

```sh
bash vps/install.sh [--package PATH] [--version vX.Y.Z] [--job-id ID] [--mode vps]
```

流程：

1. 检查 root、Linux x86_64、systemd、`flock`。
2. 读 `/etc/os-release`，只接受 Debian 12/13、Ubuntu 22.04/24.04、CentOS Stream 9/10。
3. 安装依赖：
   - Debian/Ubuntu：`ca-certificates curl python3 openvpn iproute2 iptables sstp-client ppp util-linux procps`
   - CentOS：`ca-certificates python3 iproute iptables ppp util-linux procps-ng`、EPEL、openvpn；缺少 `sstpc` 时从固定版本源码编译本机 RPM。
4. 获取部署锁 `/run/lock/vgatebox-deploy.lock`。
5. 恢复未完成事务（如果有）。
6. 下载/读取完整包和 `checksums.txt`。
7. 校验 SHA256、成员清单、版本、构建来源。
8. 解包到临时目录，调用 `installer.py install --stage ... --job-id ...`。

安装入口只接受 `vgatebox-linux-amd64.tar.gz`，不会让校验文本决定本地路径。

## 部署事务

`installer.py` 的 `OWNED` 是受管路径白名单：

```text
var/lib/vgatebox
usr/local/lib/vgatebox
etc/vgatebox
usr/local/bin/vgatebox
usr/local/bin/vgatebox-fanout
etc/default/vgatebox-fanout
etc/systemd/system/vgatebox-fanout.service
etc/systemd/system/vgatebox-pool.service
etc/systemd/system/vgatebox-pool.timer
```

`UNITS`：

```text
vgatebox-fanout.service
vgatebox-pool.timer
vgatebox-pool.service
```

事务阶段：

```text
preparing → dependencies → downloading → verifying → pool
→ stopping → backup → checking → installing → health
→ complete
   ↘ rollback（失败）
   ↘ recovery_required（自动恢复失败）
```

状态写在 `/var/lib/vgatebox-deploy/status.json`；未完成事务写在 `transaction.json`，恢复工具复制到 `recover.py`。

### 备份

- 备份目录 `/var/backups/vgatebox/install-<job-id>`，目录 0700，归档 0600。
- 只备份已存在的受管路径。
- 备份前停止服务；归档包含状态/凭据/SQLite。
- 失败时用备份恢复文件和服务状态。

### 健康检查

`Installer.health()`：

- 确认 `vgatebox-fanout.service` active；
- 读取 `settings.json`/`basepath`/`password`；
- 登录，GET `/api/console`，版本必须匹配；
- GET `/api/exits`，后端必须是 native，且面板无错误。

首次安装且工作目录为空时，健康检查成功后调用 `/api/console/direct` 开启本机网络。已有安装不会重解释直出开关。

### 恢复

`recover()`：

- 如果 journal 已 committed，删除 journal；
- 否则读取 journal，停止服务；
- `installing`/`health` 阶段恢复文件；
- 恢复服务 active/enabled 状态；
- 删除 journal。

恢复校验归档路径、硬链接、符号链接和受管路径，防止通过备份写任意文件。

## 完整包

`scripts/build_vps_package.py` 生成：

```text
VERSION
BUILD.json
package-sha256.json
bin/vgatebox-manager
bin/xray
src/                         Python 工具箱
config/settings.json
scripts/toolbox.py
scripts/vps_acceptance.py
deploy/                      安装/卸载/CLI/systemd
licenses/
UPSTREAM.md
install.sh
LICENSE
LICENSE.reference
```

要求：

- `bin/xray` 按 `config/xray-release.json` 的 SHA256 从官方归档校验。
- `VERSION` 与 `BUILD.json.version` 一致。
- `BUILD.json` 记录 `git_head`、`source_dirty`、`source_snapshot_sha256`、`manager_origin`。
- 发布包要求 `source_dirty=false`、`manager_origin=built_from_source`；`--manager` 提供的二进制标记为 `supplied_binary`，在线更新会拒绝。
- `package-sha256.json` 覆盖除自身外的所有文件；`checksums.txt` 覆盖 tar.gz。

本地构建：

```sh
cd vps/fanout/frontend && npm ci --ignore-scripts && npm run build
cd ../../..
python3 scripts/build_vps_package.py --version vX.Y.Z
```

## 在线更新

`update_manager.go`：

- 只读 GitHub Releases 最新正式版；
- 要求有 `vgatebox-linux-amd64.tar.gz` 和 `checksums.txt`；
- 只允许系统安装实例（工作目录 `/var/lib/vgatebox/fanout`、内置 Xray、存在 CLI/安装器、`systemd-run`）；
- 用 `systemd-run` 启动独立 `vgatebox-update` 单元，执行 `vgatebox update --version <tag> --job-id <id>`；
- 日志 `/var/backups/vgatebox/update-<id>/install.log`；
- 状态 `status.json`；中断显示 `interrupted`。

## 卸载

`uninstall.py`：

- 默认删除 `OWNED` + 单元；`--purge` 删除备份；`--legacy` 清理可确认归属的兼容 fanout。
- 删除前核对：服务单元内容、drop-in、挂载点、符号链接、旧安装归属、未完成事务、运行中的更新。
- 停止单元，清理每个实例的 netns/veth/iptables，删除文件，验证残留。
- 不删除外部面板、共享依赖、全局 IP 转发、系统日志、`/etc/netns`。

## 测试

```sh
python3 -m unittest tests.test_installer -v
python3 -m unittest tests.test_uninstall -v
python3 -m unittest discover -s tests -v
```

多发行版测试在一次性特权容器中运行真正的 systemd、发行版包管理器和安装器，执行安装、订阅 HTTPS 访问、更新保留和卸载。测试入口只允许在 GitHub 一次性 runner 上运行；不覆盖独立 VPS 的内核、SELinux、服务商防火墙。

## 添加一个受管路径

1. 在 `installer.py` 的 `OWNED` 和 `uninstall.py` 的 `OWNED` 同步加入路径。
2. 确认备份/恢复/卸载的归属检查能识别该路径。
3. 更新 `tests/test_installer.py`、`tests/test_uninstall.py`。
4. 更新 [状态与文件](../reference/state-and-files.md) 和 [备份与恢复](../operate/backup-restore.md)。
5. 如果路径含凭据，检查权限和日志输出。

## 安全约束

- 解包只接受普通文件/目录，拒绝绝对路径、`..`、链接、重复成员、超大包。
- 校验失败不执行任何包内脚本。
- 备份恢复只写受管路径，拒绝链接祖先和越界路径。
- 安装器不输出异常文本（可能含 URL/token），只输出错误类型和阶段。
- 服务以 root 运行（netns/iptables/OpenVPN），systemd 单元用 `ProtectSystem=strict`、`ReadWritePaths`、`NoNewPrivileges` 等收紧；不收 `PrivateDevices`/`PrivateNetwork`/`RestrictNamespaces`。

## 相关

- [安装](../operate/install.md)
- [更新](../operate/update.md)
- [卸载](../operate/uninstall.md)
- [备份与恢复](../operate/backup-restore.md)
- [测试](testing.md)
- [发布](release.md)
