# CLI 参考

vgatebox 有两个命令行入口：系统安装的 `vgatebox` 管理命令，和源码树里的 `scripts/toolbox.py` 候选构建器。所有命令都以 root 执行（除非明确说明）。

## `vgatebox`

安装后位于 `/usr/local/bin/vgatebox`，是一个 bash 包装脚本。

| 命令 | 作用 |
| --- | --- |
| `vgatebox version` | 显示管理进程版本 |
| `vgatebox check-update` | 查询 GitHub 最新正式版 |
| `vgatebox update` | 更新到最新正式版 |
| `vgatebox update --version vX.Y.Z` | 更新到指定版本 |
| `vgatebox update-status` | 查看最近一次部署/更新任务 |
| `vgatebox status` | 查看 `vgatebox-fanout.service` 和 `vgatebox-pool.timer` 状态 |
| `vgatebox refresh` | 手动刷新候选；不触发自动检测 |
| `vgatebox restart` | 重启管理服务 |
| `vgatebox access` | 显示管理地址和密码 |
| `vgatebox install --mode vps --package <tar.gz>` | 离线完整包安装 |
| `vgatebox uninstall` | 卸载 |

### `update`

```sh
vgatebox update
vgatebox update --version v1.5.7
```

等价于重新运行安装入口；会校验完整包、备份、停止服务、安装、健康检查，失败时恢复。

### `access`

```sh
vgatebox access
```

输出示例：

```text
管理地址：http://203.0.113.10:8899/aB3xY9pQ/
管理密码：<随机密码>
```

如果监听地址是 `127.0.0.1`，会提示使用 SSH 端口转发。不要把输出发到公开位置。

### `refresh`

```sh
vgatebox refresh
```

调用 `toolbox.py pool`，把新候选写到 `/var/lib/vgatebox/pool`。手动刷新会取消正在运行的检测；成功后不会额外触发自动检测。失败时保留旧候选并返回非零。

### `uninstall`

```sh
vgatebox uninstall --dry-run
vgatebox uninstall --purge --legacy
vgatebox uninstall --yes
```

参数见 [卸载](../operate/uninstall.md)。

## `vgatebox-fanout`

Go 管理进程，通常由 systemd 启动。直接运行主要用于排障和只读检查。

```sh
/usr/local/bin/vgatebox-fanout -version
/usr/local/bin/vgatebox-fanout -check-state -dir /var/lib/vgatebox/fanout
```

| 参数 | 默认 | 说明 |
| --- | --- | --- |
| `-web` | `8899` | 管理 HTTP 端口；显式给出时以命令行为准并写回设置 |
| `-max` | `16384` | 出口容量上限；必须 1..maximumSlots |
| `-dir` | `/var/lib/vgatebox/fanout` | 工作目录 |
| `-panel` | 空（等同 `native`） | 节点后端；只支持 `native`，其他值启动失败 |
| `-ip` | 空 | 公网 IPv4，用于分享链接/SOCKS5 地址；留空自动探测 |
| `-version` | — | 显示版本后退出 |
| `-check-state` | — | 只读检查状态迁移，不启动服务 |
| `-h` | — | 帮助 |

退出码：`0` 成功；`1` 运行时/校验失败；`2` 参数解析失败。

环境变量见 [配置参考](configuration.md)。

## `scripts/toolbox.py`

源码树入口，也可以使用安装后的 `/usr/local/lib/vgatebox/scripts/toolbox.py`。Python 3.9+，核心只用标准库。

```sh
python3 scripts/toolbox.py [--config PATH] [--env-file PATH] <command> [options]
```

全局参数：

| 参数 | 默认 | 说明 |
| --- | --- | --- |
| `--config` | `config/settings.json` | 候选/画像设置 |
| `--env-file` | `.env` | 画像服务环境变量 |

### `pool`

构建候选池。

```sh
python3 scripts/toolbox.py pool
python3 scripts/toolbox.py pool --csv-file /absolute/path/vpngate.csv
python3 scripts/toolbox.py pool --output /var/lib/vgatebox/pool
python3 scripts/toolbox.py pool --schedule-dir /var/lib/vgatebox/fanout/state --scheduled
```

| 参数 | 默认 | 说明 |
| --- | --- | --- |
| `--csv-file` | — | 使用本地官方格式 CSV，不访问来源 |
| `--output` | `data` | 输出目录；写 `node_pool.json`、摘要和 `configs/*.ovpn` |
| `--runtime` | `runtime` | 运行/缓存目录 |
| `--schedule-dir` | — | 周期状态目录；与 `--scheduled` 配合 |
| `--scheduled` | — | 按保存的刷新间隔决定是否真正刷新；被跳过时输出 `source_refresh_skipped` |

输出 JSON，例如：

```json
{"status":"candidate_pool_built","nodes":123}
```

失败返回 `2`，错误写到 stderr；不会输出 token 或配置内容。

### 画像命令

```sh
python3 scripts/toolbox.py profile-openvpn --ip 203.0.113.10 --country --refresh
python3 scripts/toolbox.py profile-sstp --ip 203.0.113.10 --country
python3 scripts/toolbox.py profile-socks5 --ip 203.0.113.10 --country
```

| 参数 | 说明 |
| --- | --- |
| `--ip` | 已实测的出口 IPv4；命令本身不验证隧道 |
| `--cache` | 缓存路径；默认在 `runtime/intelligence/` |
| `--country` | 独立查询国家，不推断住宅类型 |
| `--refresh` | 绕过该 IP 的缓存重新查询 |

输出画像 JSON；缺少配置或查询失败时返回 `unknown`/`not_configured`，不会伪造结论。

### `check-intelligence`

```sh
python3 scripts/toolbox.py check-intelligence
python3 scripts/toolbox.py check-intelligence --ip 203.0.113.10 --refresh
```

校验画像环境变量（URL 必须 HTTPS 且含 `{ip}`、鉴权模式合法），可选实测一个 IP。输出配置摘要和结论；**不会**回显 token、含 token 的 URL 或原始供应商响应。

## 常用组合

```sh
# 安装后看状态
vgatebox status
vgatebox access

# 刷新候选并看结果
vgatebox refresh
journalctl -u vgatebox-pool -n 50 --no-pager

# 只读检查状态文件
/usr/local/bin/vgatebox-fanout -check-state -dir /var/lib/vgatebox/fanout

# 本机探活
curl -fsS http://127.0.0.1:8899/healthz
```

## 相关

- [配置参考](configuration.md)
- [HTTP API](api.md)
- [安装](../operate/install.md)
- [更新](../operate/update.md)
- [故障排查](../operate/troubleshooting.md)
