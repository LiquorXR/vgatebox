# 开发与测试

## 本机候选池

Python 3.9+，核心使用标准库。候选构建不执行 OpenVPN 配置，也不证明节点连通：

```sh
python3 scripts/toolbox.py pool
```

生成 `data/node_pool.json`、摘要和 `data/configs/*.ovpn`；可通过 `--csv-file /absolute/path/vpngate.csv` 解析官方格式快照。配置见 `config/settings.json`。真实验活另需 curl、Xray 和对应协议依赖。

## 新控制台

```sh
cd vps/fanout/frontend
npm ci --ignore-scripts
npm run typecheck
npm run test:unit
npm run build
```

构建产物 `webdist` 不提交；CI 会在 Go 测试和打包前完成构建，并运行 Vitest 组件与页面集成测试。新控制台是默认入口，`/next/` 为别名，详见 [前端说明](FRONTEND.md)。

## 浏览器 E2E

```sh
cd vps/fanout/frontend
npm run test:e2e
```

`e2e/` 用 Playwright 跑真实后端（`playwright.config.ts`，桌面 + Pixel 5 两个项目），覆盖登录→总览、设数量→等待出口、订阅复制/二维码、删出口回 0、移动端导航与接入页签五条路径。后端须是 Linux 上以 root 起的真 manager（需 netns/iptables，Windows 本地跑不了），零隧道：无候选、目标最多到 1，直出开关走内置 Xray。CI（`e2e.yml`，需 sudo）在临时 workdir 起服务，用 `/healthz` 等就绪后跑；本地复现需自行准备 Linux 后端并导出 `E2E_BASE`、`E2E_BASE_PATH`、`E2E_PASSWORD`。E2E 不替代真实 VPS 的隧道、防火墙、SSTP 和第三方代理验收。

## 回归测试

```sh
python3 scripts/install_xray.py
python3 -m unittest discover -s tests -v
```

Python 检查解析、证据一致性、异常响应、证书拒绝、订阅清理、私密性、锁和进程清理。新控制台的纯逻辑和页面集成流程由 Vitest 检查。Linux Go 测试包含网络隔离、出口生命周期、阻断及并发检查。合成测试与真实 VPN 节点验收分别记录，不因前者通过而认定后者可用。

Go fuzz（`vps/fanout/fuzz_test.go`，纯函数无网络磁盘）：`parseNodeCSV`、`clashProxy`、`normalizePolicy`、`normalizeInboundSpec`，平时 `go test` 只跑种子语义断言；延长跑 `go test -fuzz=FuzzParseNodeCSV -fuzztime=5m vps/fanout`（余类推），每夜全量 race 会带上种子部分。

## GitHub Actions

- `test.yml`：Python 3.9/3.12、前端类型检查/组件与页面集成测试/构建。
- `e2e.yml`：Linux 真后端浏览器 E2E（桌面 + 移动端），需 sudo 起服务。
- `vps.yml`：Linux Go/race、卸载验证、完整包构建及多发行版安装矩阵。
- 手动运行 `vps.yml` 时可选 `live_mirror`，在一次性 Linux 环境运行生产 Builder，自行请求公开镜像索引与固定提交 CSV；不发布候选、不连接 VPN、不替代用户 VPS 验收。
- `release-vps.yml`：手动发布通过测试的完整包，核对来源、版本与文件摘要。
- `update.yml`：仅手动构建共享候选池，不定时运行。

多发行版测试在一次性特权容器中运行真正的 systemd、发行版包管理器和安装器，执行安装、实际订阅 HTTPS 访问、更新保留与卸载。测试入口仅允许在 GitHub 一次性 runner 上运行；它不覆盖独立 VPS 的内核、SELinux 或服务商防火墙。范围见 [系统兼容性](LINUX_SUPPORT.md)。

周期任务用受控时间和临时状态验证刷新默认 60 分钟/最低 30 分钟、检测默认 30 分钟/最低 15 分钟、开关、失败退避、重启保留、等待来源刷新完成与手动任务互斥；结果有效期按检测间隔和实测轮次计算，覆盖下一轮检测；Python 和 Go 共享刷新状态，避免系统定时器重复请求来源。测试不向 VPN Gate 做高频检测。

Clash 输出用固定官方 Mihomo 测试内核验证配置，覆盖 VLESS 的 13 个可表达的「传输 × 安全层」组合（TCP、gRPC、XHTTP 支持 none/tls/reality；WebSocket、HTTPUpgrade 支持 none/tls）、非 TCP 传输上的 Vision `flow`、VLESS Encryption 和 mKCP 拒绝路径；旧 VMess/Trojan/Hysteria2 入口的迁移过滤也有单元测试，UDP 通过 SOCKS5 回环中继测试验证。Debian 12 隔离安装测试还使用真实订阅连接本机 VLESS（TCP + REALITY + VLESS Encryption + Vision），校验 HTTPS 出口与内容，并确认选中代理失效时不回落 DIRECT。测试内核仅下载到 CI，不进入产品安装包。GUI 版本未提供，不将内核测试视为所有 Clash Verge 版本已验收。

## 记录与参考

测试结果与限制见 [系统兼容性](LINUX_SUPPORT.md)；上游节点实测不保证持续在线。

接口资料：[VPN Gate](https://www.vpngate.net/en/)、[Xray](https://xtls.github.io/en/config/transport.html)、[ipify](https://www.ipify.org/)、[icanhaz](https://github.com/major/icanhaz)、[IPLocate](https://www.iplocate.io/docs/getting-started/authentication)。
