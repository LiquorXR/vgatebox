# 状态与文件

vgatebox 的持久化分为**受管程序**、**配置**、**私有状态**、**缓存**、**运行态**和**备份**。排障时先看这里的路径和权限。

所有工作目录默认 `0700`，状态/配置文件默认 `0600`；安装器使用 `umask 077`。

## 系统路径

```text
/usr/local/lib/vgatebox/                程序、部署脚本、Python 工具箱、内置配置模板
/usr/local/bin/vgatebox                 管理 CLI
/usr/local/bin/vgatebox-fanout          Go 管理进程
/etc/vgatebox/pool.json                 候选/画像设置（0600）
/etc/vgatebox/intelligence.env          画像服务环境（0600）
/etc/default/vgatebox-fanout            运行时环境变量（0600）
/etc/systemd/system/vgatebox-fanout.service
/etc/systemd/system/vgatebox-pool.service
/etc/systemd/system/vgatebox-pool.timer
/etc/netns/                             共享的持久 netns 配置目录（0755，卸载不删）
/run/netns/                             运行中的命名空间（tmpfs）
/var/lib/vgatebox/pool/                 候选池输出
/var/lib/vgatebox/fanout/               工作目录
/var/lib/vgatebox-deploy/               部署事务和更新状态
/var/backups/vgatebox/                  安装/更新备份
```

## 工作目录

```text
/var/lib/vgatebox/fanout/
  state/                 意图、设置、检测、证据
  cache/                 画像/国家缓存
  certs/                 自签证书
  run/                   隧道配置、日志、Xray 运行态
  lock                   实例锁（防止同一目录跑两个实例）
  panel_mode             兼容后端哨兵；只允许空或 native
  bin/xray               内置 Xray 二进制
```

### `state/`

| 文件 | 用途 |
| --- | --- |
| `settings.json` | 管理端口、监听地址、来源过滤、订阅 token |
| `password` | 管理密码（明文文件，0600） |
| `basepath` | 随机访问路径 |
| `state.json` | 出口列表、端口、节点、凭据、策略、停止意图 |
| `console.json` | 目标数量、策略、REALITY 目标、待删除 Slot |
| `native.json` | 入站、客户端、绑定、VLESS Encryption |
| `native.previous.json` | 上一次内置配置，用于回滚 |
| `xray.json` | 当前生成的 Xray 运行配置 |
| `check-settings.json` | 检测并发、IP 类型开关、TTL |
| `automation.json` | 自动刷新开关/间隔 |
| `check-schedule-state.json` | 自动检测进度 |
| `refresh-state.json` | 来源刷新状态 |
| `sstp.json` | SSTP 设置和结果 |
| `third-party-checks.json` | 第三方代理检测结果 |
| `slot_health.json` | 出口观察快照（文件模式） |
| `ip-type-cache.json` | IP 类型缓存（文件模式） |
| `checks.json` | 兼容检测结果；SQLite 模式下可能不再使用 |
| `vgatebox.db` | SQLite：检测结果、出口观察、IP 类型缓存（含 `-wal`/`-shm`） |
| `netbase` | 实例网段基数（`10.<base>.0.0/16`） |
| `manual-refresh.json` | CLI 手动刷新标记，调度器消费后删除 |
| `.management-transaction.json` | 管理设置事务日志 |
| `.state-transaction.json` | 出口状态事务日志 |
| `.native-transaction.json` | 内置入站事务日志 |
| `.console-transaction.json` | 出口计划事务日志 |

意图类文件（`state.json`、`console.json`、`native.json`、`settings.json`、`password`、`basepath`）是恢复依据，**不要手工编辑**。读不懂或版本过新时会拒绝启动，避免猜错绑定导致流量直出。

### `cache/`

```text
cache/intelligence/cache.json          OpenVPN 画像缓存
cache/intelligence/sstp-cache.json     SSTP 画像缓存
cache/intelligence/socks5-cache.json   第三方 SOCKS5 画像缓存
cache/country/cache.json               国家查询缓存
```

缓存可再生；删除后下一次查询重建，不会影响出口意图。缓存文件 0600。

### `certs/`

TLS 入站的自签证书和私钥。删除后已分发的自签 TLS 链接会失效；REALITY/无 TLS 入口不受影响。证书文件 0600。

### `run/`

```text
run/<namespace>.ovpn      出口的 OpenVPN 配置
run/auth.txt              OpenVPN 认证文件
run/<namespace>.log       出口日志
run/xray.log              内置 Xray 日志
run/xray.pid              Xray PID
run/.connectivity-*       临时检测目录（失败/取消后可能残留）
run/sstp-*                SSTP 探测任务
```

运行态不是恢复依据；重启会重建。`.connectivity-*` 目录可安全清理，但先停止服务，避免删到正在运行的任务。

### `lock` 与 `panel_mode`

- `lock` 是工作目录的实例锁；同一目录只允许一个实例。第二个实例会启动失败。
- `panel_mode` 是兼容外部后端的哨兵；内容必须为空或 `native`。非 `native` 值（如 `3x-ui`、`xray-cf-lite`）会让服务拒绝启动，避免外部面板仍占用端口。不要手工改成其他值。

## 候选池

```text
/var/lib/vgatebox/pool/
  node_pool.json           候选清单（含 source、generated_at、nodes）
  node_pool.json.sha256    清单摘要
  configs/*.ovpn           候选配置，按配置摘要命名
```

- 候选池由 `toolbox.py pool` 写入；失败时保留旧文件。
- `node_pool.json` 的 `source` 记录来源、镜像提交、快照时间、校验和解析错误。
- 候选配置只是数据，不会被执行；出口建连时才使用。

## 部署状态与备份

```text
/var/lib/vgatebox-deploy/
  status.json        最近一次部署/更新任务（网页更新状态）
  transaction.json   未完成事务的恢复记录
  recover.py         恢复工具副本
```

```text
/var/backups/vgatebox/
  install-<job-id>/project.tar.gz
  update-<id>/install.log
```

- `transaction.json` 存在且未提交时，安装器/卸载器会先恢复或拒绝操作；不要手工删除。
- 备份只覆盖受管文件，不是整机镜像；详细说明见 [备份与恢复](../operate/backup-restore.md)。

## 权限

| 路径 | 权限 |
| --- | --- |
| 工作目录、`state/`、`cache/`、`certs/`、`run/` | 0700 |
| `settings.json`、`password`、`basepath`、状态/配置文件 | 0600 |
| `vgatebox.db` 及 `-wal`/`-shm` | 0600 |
| `bin/xray` | 0700 |
| `/usr/local/bin/vgatebox*` | 0755 |
| systemd 单元 | 0644 |
| `/var/backups/vgatebox` | 0700 |
| 备份归档 | 0600 |
| `/etc/netns` | 0755（共享目录，卸载不删） |

备份包含凭据和私钥；限制访问，不要上传到不可信存储。

## 排障入口

| 想看什么 | 看哪里 |
| --- | --- |
| 服务是否在跑 | `systemctl status vgatebox-fanout` |
| 启动/健康/错误日志 | `journalctl -u vgatebox-fanout` |
| Xray 日志 | `run/xray.log` |
| 出口 OpenVPN 日志 | `run/<namespace>.log` |
| 测试/下载日志 | `journalctl -u vgatebox-pool` |
| 当前状态/版本 | `/healthz`、`/api/console` |
| 出口状态机 | `state/state.json` + `slot.phase` 日志 |
| 更新状态 | `vgatebox update-status`、`/var/lib/vgatebox-deploy/status.json` |
| 网络资源 | `ip netns list`、`iptables -S`、`ss -lntup` |

日志只记录方法、路径、状态和耗时；不记录 query（可能含订阅 token）、body（可能含密码）和请求头。

## 相关

- [配置参考](configuration.md)
- [CLI 参考](cli.md)
- [备份与恢复](../operate/backup-restore.md)
- [安全](../operate/security.md)
- [故障排查](../operate/troubleshooting.md)
