# 测试

vgatebox 的测试分四层：纯逻辑/单元、页面集成、真实后端 E2E、特权发行版安装。前两层可在普通开发机跑；后两层需要 Linux、root、network namespace 和 systemd。

## 快速门禁

```sh
sh scripts/check.sh
```

- Linux：`GOOS=linux go vet`、Go 测试、Python unittest、前端 typecheck/Vitest/build。
- Windows/macOS：只做 `GOOS=linux` 交叉编译检查和前端测试；Linux-only 的 netns/pidfd/权限用例交给 CI。
- `node_modules` 不存在时跳过前端；先 `npm ci --ignore-scripts`。

## Go

```sh
cd vps/fanout

# 交叉检查（Windows 本地可做）
GOOS=linux GOARCH=amd64 go vet ./...
GOOS=linux GOARCH=amd64 go test -c -o /tmp/check .

# Linux 完整测试
go test ./...

# 竞态（建议 Linux）
go test -race ./...

# 指定契约测试
go test -run TestAPIContract .
```

Windows 本地不能直接跑完整 Go 测试：二进制含 Linux-only 的 netns/pidfd/权限代码。用 WSL2 或 CI。

### 覆盖重点

- 启动阶段与退出码（`boot_test.go`）。
- 路由契约 62/62、金样本、匿名 handler 检查（`api_contract_test.go`、`routes.go`）。
- Slot 状态机转换（`slot_phase_test.go`）。
- 出口生命周期、删除、恢复、停止（`console_*_test.go`、`manager_lifecycle.go` 相关测试）。
- 检测/证据/TTL（`console_checks*`、`console_ttl_test.go`、`egress_test.go`）。
- 健康检查、重连、换节点 worker（`health_test.go`、`pending_test.go`）。
- network namespace、DNS、kill switch、端口/容量（`netns*_test.go`、`killswitch_test.go`、`capacity_test.go`）。
- 订阅/Clash 转换（`sub_test.go`、`sub_clash_test.go`）。
- 第三方代理、SSTP（`third_party_test.go`、`sstp_live_test.go`）。
- 更新事务（`update_test.go`、`update_manager_test.go`）。
- 状态持久化/迁移（`state_test.go`、`migration_check_test.go`、`schema_test.go`）。

### Fuzz

```sh
cd vps/fanout
go test -fuzz=FuzzClashProxy -fuzztime=5m
go test -fuzz=FuzzNormalizePolicy -fuzztime=5m
go test -fuzz=FuzzNormalizeInboundSpec -fuzztime=5m
```

目标都是纯函数，无网络/磁盘；平时 `go test` 只跑种子语义断言，夜间 full race 会带上种子。

## Python

```sh
python3 -m unittest discover -s tests -v
```

或单文件：

```sh
python3 -m unittest tests.test_toolbox -v
python3 -m unittest tests.test_installer -v
python3 -m unittest tests.test_uninstall -v
```

`tests/test_installer.py` 需要 Xray 二进制时，先运行：

```sh
python3 scripts/install_xray.py
```

覆盖：

- CSV 解析、候选池构建、来源回退、镜像完整性。
- 周期调度、刷新状态、重启保留。
- 国家/画像证据、异常响应、证书拒绝。
- 订阅清理、私密性、锁和进程清理。
- 安装/更新/回滚、卸载/归属/网络资源。
- VPS 验收脚本（真实 VPS 上运行，不在单测里）。

## 前端

```sh
cd vps/fanout/frontend
npm ci --ignore-scripts
npm run typecheck
npm run test:unit
npm run build
```

Vitest 覆盖纯逻辑、API 客户端、共享组件、登录页和页面集成（mock `fetch`）。页面集成覆盖主流程，不替代真实 VPS 的隧道/防火墙/代理验收。

### E2E

```sh
cd vps/fanout/frontend
npm run test:e2e
```

Playwright 跑真实后端（Linux，需 root/netns/iptables），桌面 + Pixel 5 两个项目，覆盖：

- 登录 → 总览；
- 设置目标数量 → 等待出口；
- 订阅复制/二维码；
- 删除出口回 0；
- 移动端导航与接入页签。

CI 的 `e2e.yml` 在临时 workdir 起服务，零隧道：无候选、目标最多到 1，直出开关走内置 Xray；用 `/<basepath>/healthz` 就绪后跑。本地复现需准备 Linux 后端并导出 `E2E_BASE`、`E2E_BASE_PATH`、`E2E_PASSWORD`。

## CI 工作流

| 工作流 | 触发 | 覆盖 |
| --- | --- | --- |
| `test.yml` | push/PR | 前端 typecheck/Vitest/build；Python 3.9/3.12 unittest（含 Xray 安装） |
| `lint.yml` | push/PR/手动 | `go vet`、staticcheck v0.8.2、govulncheck v1.9.0 第三方门禁、生产 npm audit |
| `e2e.yml` | push/PR/手动 | 真实后端 Playwright（桌面 + 移动端），零隧道 |
| `vps.yml` | push/PR/tag/手动 | Linux Go 测试（sudo）、race 子集、卸载验证、完整包构建、Debian/Ubuntu 四系统安装矩阵；tag 自动发布 |
| `nightly-race.yml` | 每日/手动 | 全量 `go test -race`（30 分钟超时，job 45 分钟） |
| `release-vps.yml` | 手动 | 从一次成功的 `vps.yml` 运行补发已测试的包 |
| `update.yml` | 手动 | 仅构建共享候选池产物，不定时运行 |
| `docs.yml` | push/PR | 文档断链、孤儿页、禁用词、API 路由漂移检查 |

### `vps.yml` 的细节

- 先构建前端，再 `go test -c`，用 `sudo` 运行编译后的测试二进制（netns/iptables 需要 root）。
- `-race` 只跑与网络/检测/并发相关的子集，避免 PR 超时；全量 race 在 nightly。
- `tests/uninstall_linux.py` 验证真实隔离网络资源清理；`tests/uninstall_systemd.py` 验证 systemd 停止/重置。
- `distro-install` 在一次性特权容器中跑 Debian 12/13、Ubuntu 22.04/24.04 的安装、订阅 HTTPS 访问、更新保留和卸载；Debian 12 额外用 Mihomo 客户端验证。
- tag 构建要求 `BUILD.json.git_head == 当前提交`、`source_dirty=false`、`manager_origin=built_from_source`，否则不发布。

## 多发行版测试

```sh
# 仅 GitHub 一次性 runner
bash tests/distro_install.sh debian:12
```

它在特权容器中启动真正的 systemd、发行版包管理器和安装器，执行：

- 安装；
- 实际订阅 HTTPS 访问；
- 更新保留；
- 卸载；
- Debian 12 用 Mihomo 验证真实 VLESS（TCP + REALITY + VLESS Encryption + Vision），并确认选中代理失效时不回落 DIRECT。

不覆盖独立 VPS 的内核、SELinux、服务商防火墙；这些需要真实 VPS 验收。

## 真实 VPS 验收

仓库里的 `src/vpngate_toolbox/vps_acceptance.py` / `tests/test_vps_acceptance.py` 用于真实 VPS 验收：登录、状态对比、订阅、出口、SSTP、第三方代理、防火墙等。它和合成测试分开记录；合成测试通过不代表真实上游可用。

建议验收清单：

- 管理页登录、随机路径、改密码/端口；
- 本机网络订阅 + 客户端；
- VPN Gate 出口创建/就绪/换 IP/删除；
- 第三方 SOCKS5 检测/订阅；
- 节点刷新/检测/周期任务；
- 更新/备份/卸载；
- 反向代理 HTTPS、移动端、慢网；
- SSTP 客户端。

## 测试数据

- `vps/fanout/testdata/api/routes.json`：路由金样本。
- `vps/fanout/testdata/api/payloads.json`：API payload 契约。
- `vps/fanout/testdata/migration-v031/`：旧版状态迁移夹具。
- `tests/mihomo-release.json`：CI 固定 Mihomo 版本。
- `tests/pool_source_fixture.py`、`tests/test_source_fallback.py`：来源/镜像夹具。

修改契约或迁移格式时同步更新这些夹具。

## 常见失败

| 现象 | 原因 |
| --- | --- |
| Windows 上 `go test` 编译失败 | Linux-only 代码；用 `GOOS=linux go test -c` 或 WSL2/CI |
| Go 测试需要 root | netns/iptables/pidfd；用 `sudo` 或 CI |
| Playwright 起不来 | 需要 Linux root 后端、Xray 二进制、`E2E_*` 环境变量 |
| Vitest 失败但页面正常 | mock API/类型漂移；先 `npm run typecheck` |
| `npm audit` 只查生产 | CI 只审 `--omit=dev`；dev 依赖问题单独评估 |
| distro 测试跳过 | 只允许 GitHub 一次性 runner；不要在真实 VPS 跑内层脚本 |

## 相关

- [后端](backend.md)
- [前端](frontend.md)
- [安装器](installer.md)
- [发布](release.md)
- [边界与兼容](../reference/limits.md)
