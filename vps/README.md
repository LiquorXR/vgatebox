# vgatebox VPS Standalone

管理进程、内置 Xray、共享候选池都在 VPS 本地运行。节点链接后端固定为内置 Xray，不再支持 3x-ui / xray-cf-lite 等外部面板。要求 Linux amd64、root 和 systemd。支持范围见 [运维](../docs/OPS.md)。

如果工作目录还留有 `panel_mode`，内容必须是 `native` 或为空；旧值会让服务启动失败。

## 安装与升级

均以 root 执行，见 [运维](../docs/OPS.md)：

```sh
curl -fsSL https://github.com/LiquorXR/vgatebox/releases/latest/download/install.sh -o /tmp/vgatebox-install.sh && bash /tmp/vgatebox-install.sh && vgatebox access
```

`vgatebox access` 查看管理地址和密码，`vgatebox update` 更新。备份仅覆盖受管文件，重装前另存到本机。

离线包：`bash vps/install.sh --mode vps --package /path/vgatebox-linux-amd64.tar.gz`，相邻目录须有 `checksums.txt`。

DNS：每个出口在自己 namespace 内经 `tun0` 解析，默认 `1.1.1.1,8.8.8.8`，可用 `FANOUT_DNS_SERVERS` / `FANOUT_DNS_TTL` 调整。

反向代理：设置 `FANOUT_PUBLIC_URL=https://panel.example.com`，代理须透传 `X-Forwarded-Proto: https`。详见 [前端](../docs/FRONTEND.md)。

## 首次管理和订阅

完整步骤见 [首次使用](../docs/FIRST_USE.md)，代理操作见 [代理资源](../docs/PROXY.md)。

1. `vgatebox access` 登录。
2. 先启用「VPS 本机网络」验证客户端。
3. 「订阅」复制链接，v2rayN 用默认格式，Clash 用 Clash/Mihomo 格式。
4. 需要 VPN Gate 时在「出口」设数量，等待「已就绪」后更新订阅。

## 本地刷新

「节点池 → 周期任务」设刷新（默认 60 分钟、最低 30 分钟）和检测。`vgatebox refresh` 或手动刷新不受自动开关限制。来源配置在 `/etc/vgatebox/pool.json`。

节点池「检测 IP 类型」开关默认关闭；出口建连始终查询 IP 类型，不受该开关影响。

## OpenVPN 出口画像

每次建连取 namespace 内两个 HTTPS Echo 的基线 IP，经 intelligence adapter 分类，再核对 SOCKS actual，三者一致才发布证据。

安装器默认写入 IPPure（`IPPURE_ENABLED=1`）。查询失败或证据过期显示 `unknown`，不继承其他画像。

国家另查 `https://api.country.is/{ip}`，证据有效一天，只定国家，不管住宅类型。

`/etc/vgatebox/intelligence.env`（0600）：

```ini
INTELLIGENCE_PROVIDER=auto
IPPURE_ENABLED=1
IPPURE_URL=https://my.ippure.com/v1/info
IPPURE_TIMEOUT_SECONDS=15
INTELLIGENCE_URL_TEMPLATE=
INTELLIGENCE_TOKEN=
INTELLIGENCE_AUTH_MODE=auto
```

`PROVIDER`：`auto` 保持旧行为，`ippure` 强制 IPPure，`custom` 只用自定义模板，`off` 关闭检测。改文件下次建连生效，无需重启。

自定义模板示例：

```ini
INTELLIGENCE_PROVIDER=custom
INTELLIGENCE_URL_TEMPLATE=https://<主机>/lookup/{ip}
INTELLIGENCE_TOKEN=<可选>
INTELLIGENCE_AUTH_MODE=auto
```

URL 须 HTTPS 且含 `{ip}`，鉴权 `header` / `bearer` / `query` / `none`。校验：

```sh
python3 scripts/toolbox.py --config config/settings.json --env-file /path/intelligence.env check-intelligence --ip 1.2.3.4 --refresh
```

## Slot 策略

新建或“策略”按钮修改，保存后重新验活。策略存于私有 `state.json`，示例：

```json
{
  "country": "US",
  "allowed_ip_types": ["strict_residential", "unknown"],
  "maximum_latency_ms": 1500
}
```

国家留空不限；类型只有 `strict_residential` / `unknown`。最终以本轮真实出口画像为准，缺证据则等待，不用来源信息补齐。

## 支持的接入协议

内置 Xray 只保留 VLESS。自动入口默认 VLESS + VLESS Encryption（X25519）+ RAW/TCP + REALITY + Vision，REALITY 目标默认 `www.tesla.com`。Mihomo 无法表示 VLESS + mKCP，该类只进 v2rayN 订阅。

## 卸载

```sh
vgatebox uninstall --dry-run --purge --legacy
```

省略 `--purge` 保留备份。详见 [运维](../docs/OPS.md)。
