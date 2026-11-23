# 配置参考

vgatebox 的配置分三层：**命令行参数**、**环境变量**、**落盘设置/状态文件**。改配置前先确认优先级和生效时机；状态文件不要手工编辑。

## 优先级

一般顺序：

```text
显式命令行参数 > 环境变量 > 落盘设置 > 内置默认值
```

例外：

- `FANOUT_RESIDENTIAL_ONLY=0` 会覆盖设置页的 `residential_only`。
- `-web` 显式给出时覆盖 `state/settings.json` 里的端口，并写回。
- `-ip` 覆盖 `FANOUT_PUBLIC_IP`。
- 修改 `intelligence.env` 后，下一次画像查询生效，不需要重启。
- 修改 `pool.json` 后，下一次候选刷新生效。

## 环境变量

### 运维/运行时

| 变量 | 默认 | 作用 |
| --- | --- | --- |
| `FANOUT_PUBLIC_URL` | 空 | 订阅/分享链接的外部 origin，如 `https://panel.example.com`；不要带路径 |
| `FANOUT_PUBLIC_IP` | 自动探测 | 公网 IPv4，用于分享链接、SOCKS5 地址；也用于拒绝 namespace 回落到本机 IP |
| `FANOUT_TRUSTED_PROXIES` | 空 | 可信反向代理 IP/CIDR，逗号分隔；本机/私有网段默认可信 |
| `FANOUT_HSTS` | 空（不发） | 设为 `1` 时后端在 HTTPS 下发 HSTS；有反向代理时保持空，由代理层发 |
| `FANOUT_VALIDATED_POOL_FILE` | `/var/lib/vgatebox/pool/node_pool.json` | 候选池文件 |
| `FANOUT_POOL_CONFIG` | `/etc/vgatebox/pool.json` | 候选/画像设置 |
| `FANOUT_INTELLIGENCE_ENV_FILE` | `/etc/vgatebox/intelligence.env` | 画像服务环境文件 |
| `FANOUT_TOOLBOX_SCRIPT` | `/usr/local/lib/vgatebox/scripts/toolbox.py` | 工具箱脚本路径 |
| `FANOUT_SLOT_PORT_BASE` | 空（`20000–59999` 随机；系统安装后默认为 `17928`） | 固定 Slot 端口基址；设置后使用基址–65535 |
| `FANOUT_DNS_SERVERS` | `1.1.1.1:53,8.8.8.8:53` | 出口 namespace 内的 DNS；支持 `ip` 或 `ip:port` |
| `FANOUT_DNS_TTL` | `60` | DNS 缓存秒数 |
| `FANOUT_RESIDENTIAL_ONLY` | 未设置 | 设为 `0` 强制关闭“只列志愿者节点”的来源过滤；否则以设置页为准 |
| `FANOUT_INITIAL_REGION` | 空 | 首次启动且无隧道时，用该国家创建 slot 1；之后不再生效 |
| `FANOUT_INITIAL_NODE` | 空 | 首次启动时指定节点主机名；优先于 `FANOUT_INITIAL_REGION` |
| `FANOUT_NETNS_CHILD` | 内部 | 子进程标记，**不要手动设置** |

安装器写入的 `/etc/default/vgatebox-fanout` 默认值：

```ini
FANOUT_VALIDATED_POOL_FILE=/var/lib/vgatebox/pool/node_pool.json
FANOUT_SLOT_PORT_BASE=17928
```

`FANOUT_INITIAL_REGION`/`FANOUT_INITIAL_NODE` 只用于首次启动的确定性引导；常规数量/策略由控制台管理。

### 画像服务

写入 `/etc/vgatebox/intelligence.env`（0600），或通过 `FANOUT_INTELLIGENCE_ENV_FILE` 指定：

| 变量 | 默认 | 作用 |
| --- | --- | --- |
| `INTELLIGENCE_PROVIDER` | `auto` | `auto`、`ippure`、`custom`、`off` |
| `IPPURE_ENABLED` | `1` | IPPure 开关；`PROVIDER=auto` 时生效 |
| `IPPURE_URL` | `https://my.ippure.com/v1/info` | IPPure 查询地址 |
| `IPPURE_TIMEOUT_SECONDS` | `15` | IPPure 超时 |
| `INTELLIGENCE_URL_TEMPLATE` | 空 | 自定义画像 URL，必须 HTTPS 且含 `{ip}` |
| `INTELLIGENCE_TOKEN` | 空 | 自定义服务 token；不要提交到仓库 |
| `INTELLIGENCE_AUTH_MODE` | `auto` | `auto`、`header`、`bearer`、`query`、`none` |
| `INTELLIGENCE_AUTH_HEADER` | `X-API-Key` | header 模式的请求头名 |
| `INTELLIGENCE_TOKEN_PARAM` | `token` | query 模式的参数名 |
| `COUNTRY_URL_TEMPLATE` | `https://api.country.is/{ip}` | 国家查询；设空关闭 |

行为：

- `auto`：有 `IPPURE_ENABLED=1` 走 IPPure；否则走自定义模板。
- `ippure`：强制 IPPure。
- `custom`：只用自定义模板；未配置时失败。
- `off`：关闭全部画像查询；出口类型为 `unknown`。
- URL 必须 HTTPS 且含 `{ip}`；鉴权模式校验失败会快速报错，不发起查询。
- 国家查询与 IP 类型查询分开；国家证据不能证明住宅类型。

### 开发/测试

| 变量 | 默认 | 作用 |
| --- | --- | --- |
| `VGATEBOX_API_TARGET` | `http://127.0.0.1:8899` | Vite 开发代理目标 |
| `VGATEBOX_UPDATE_API_GOLDEN` | 空 | 仅测试用：重写 API 路由金样本 |
| `E2E_BASE` / `E2E_BASE_PATH` / `E2E_PASSWORD` | — | Playwright E2E 连接真实后端 |

不要把测试变量写进生产环境。

## `/etc/vgatebox/pool.json`

候选构建器设置，默认来自 `config/settings.json`：

```json
{
  "source_urls": [
    "https://www.vpngate.net/api/iphone/",
    "https://download.vpngate.jp/api/iphone/"
  ],
  "pool_source_timeout_seconds": 10,
  "mirror_fallback": { "enabled": true, "max_age_hours": 24 },
  "countries": [],
  "node_timeout_seconds": 30,
  "intelligence_ttl_seconds": 86400,
  "intelligence_query_limit": 50
}
```

| 字段 | 默认 | 说明 |
| --- | --- | --- |
| `source_urls` | 两个官方 URL | 按顺序尝试；全部失败才走镜像（若启用） |
| `pool_source_timeout_seconds` | `10` | 单次来源请求超时 |
| `mirror_fallback.enabled` | `true` | 官方来源失败时是否使用镜像 |
| `mirror_fallback.max_age_hours` | `24` | 镜像快照最大年龄 |
| `countries` | `[]` | 额外国家过滤；空表示不额外限制 |
| `node_timeout_seconds` | `30` | 画像/HTTP 请求超时 |
| `intelligence_ttl_seconds` | `86400` | 画像缓存秒数 |
| `intelligence_query_limit` | `50` | 单轮画像查询上限 |

修改后下一次刷新生效。`/etc/vgatebox/pool.json` 权限 0600；不要放入 token 或私密 URL。

## 运行设置

`/var/lib/vgatebox/fanout/state/settings.json`（0600）：

```json
{
  "port": 8899,
  "listen_addr": "0.0.0.0",
  "residential_only": false,
  "sub_token": "<订阅口令>"
}
```

| 字段 | 默认 | 说明 |
| --- | --- | --- |
| `port` | `8899` | 管理端口；设置页可改 |
| `listen_addr` | `0.0.0.0` | 空/`0.0.0.0` 所有网卡；`127.0.0.1` 只本机；同端口换地址可在设置页直接改 |
| `residential_only` | 首次安装写入 `false` | 是否只列志愿者节点；设置页可开启；`FANOUT_RESIDENTIAL_ONLY=0` 可强制关闭 |
| `sub_token` | 自动生成 | 订阅口令；重置后旧地址失效 |

管理密码和访问路径分别在 `state/password`、`state/basepath`，不放在 `settings.json`。

## 状态与意图文件

这些文件由程序维护，**不要手工编辑**：

| 文件 | 内容 |
| --- | --- |
| `state/state.json` | 出口列表、端口、节点、凭据、策略、停止意图 |
| `state/console.json` | 目标数量、策略、REALITY 目标、待删除 Slot |
| `state/native.json` | 入站、客户端、绑定、VLESS Encryption |
| `state/native.previous.json` | 上一次配置，用于回滚 |
| `state/checks.json` | 兼容检测结果；SQLite 模式下可能不再使用 |
| `state/check-settings.json` | 检测并发、IP 类型开关、TTL |
| `state/automation.json` | 自动刷新开关/间隔 |
| `state/check-schedule-state.json` | 自动检测进度 |
| `state/refresh-state.json` | 来源刷新状态 |
| `state/sstp.json` | SSTP 设置和结果 |
| `state/third-party-checks.json` | 第三方代理检测结果 |
| `state/slot_health.json` | 出口观察快照（文件模式） |
| `state/ip-type-cache.json` | IP 类型缓存（文件模式） |
| `state/vgatebox.db` | SQLite：检测结果、出口观察、IP 类型缓存 |
| `state/netbase` | 实例网段基数 |
| `state/manual-refresh.json` | CLI 手动刷新的覆盖标记 |

意图类文件（`state.json`、`console.json`、`native.json`）读不懂时 fail-closed：拒绝启动或保留原文件，不会猜测。检测/画像类数据可再生，SQLite 打不开时回退旧 JSON，fail-open。

## 默认值总表

| 项目 | 默认 |
| --- | --- |
| 管理端口 | `8899` |
| 管理监听 | `0.0.0.0` |
| 工作目录 | `/var/lib/vgatebox/fanout` |
| 出口容量 `-max` | `16384` |
| 默认目标数量 | `6`（仅在无保存数量、无旧出口时） |
| 自动刷新 | 60 分钟；最低 30 分钟 |
| 检测结果有效期 | 30 分钟 |
| 排序观察有效期 | 30 分钟 |
| IP 类型缓存 | 24 小时 |
| 检测并发 | 2 |
| 内部 SOCKS5 端口 | `20000–59999` 随机；设置基址后基址–65535 |
| 自动入站端口 | 优先 `30000 + Slot`，冲突时随机 |
| SSTP 有效期 | 60 分钟 |
| SSTP 周期 | 0（关闭） |
| SSTP 超时 | 90 秒 |
| 登录会话 | 12 小时 |
| 登录失败锁定 | 8 次 / 2 分钟 |
| 订阅 token 长度 | 20 字节（40 个十六进制字符） |

## 校验配置

```sh
# 画像配置（不泄露 token）
python3 scripts/toolbox.py check-intelligence

# 带实测 IP
python3 scripts/toolbox.py check-intelligence --ip 203.0.113.10 --refresh

# 只读检查状态迁移
/usr/local/bin/vgatebox-fanout -check-state -dir /var/lib/vgatebox/fanout
```

修改环境文件后：

```sh
systemctl restart vgatebox-fanout
```

画像文件通常不需要重启，下一次查询生效；涉及监听地址/端口/访问路径的修改以设置页为准。

## 相关

- [CLI 参考](cli.md)
- [状态与文件](state-and-files.md)
- [边界与兼容](limits.md)
- [安全](../operate/security.md)
- [安装](../operate/install.md)
