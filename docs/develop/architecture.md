# 架构

vgatebox 是“控制面 + 数据面”的单机系统：Go 管理进程维护出口和内置 Xray，Python 工具箱构建候选池，Vue 控制台由 Go 二进制内嵌提供，安装器负责系统事务。

## 组件

```text
┌──────────────────────────────────────────────────────────────┐
│ Go 管理进程 vgatebox-fanout                                  │
│                                                              │
│  HTTP 控制台 / API / 订阅                                     │
│    ├─ Auth + 随机访问路径 + 安全头                            │
│    ├─ Console：意图、检测、自动化、SSTP、第三方代理           │
│    └─ Manager：Slot 生命周期、健康检查、换节点                │
│                                                              │
│  数据面                                                       │
│    ├─ 每 Slot：netns + veth + tun0 + OpenVPN + 127.0.0.1 SOCKS│
│    ├─ Native：内置 Xray 配置、入站、客户端、VLESS Encryption  │
│    └─ 路由：入站 → 出站（Slot / 第三方 / 直出）               │
│                                                              │
│  持久化                                                       │
│    ├─ JSON 意图：state/console/native/settings                │
│    ├─ SQLite：检测结果、出口观察、IP 类型缓存                 │
│    └─ 事务日志 + 恢复                                         │
└──────────────────────────────────────────────────────────────┘
        ▲                                   ▲
        │ systemd                           │ HTTP
        │                                   │
┌───────┴────────┐                  ┌───────┴────────┐
│ Python toolbox │                  │ 浏览器 / 客户端 │
│ 候选池 / 画像   │                  │ v2rayN / Clash │
└────────────────┘                  └────────────────┘
```

| 组件 | 入口 | 职责 |
| --- | --- | --- |
| Go 管理进程 | `vps/fanout/main.go` | 启动编排、Manager、Console、HTTP、订阅、更新 |
| 内置 Xray | `vps/fanout/native*.go` | VLESS 入站、出站、路由、REALITY/VLESS Encryption |
| 隧道 | `vps/fanout/tunnel*.go`、`netns*.go` | network namespace、OpenVPN、SOCKS5、kill switch |
| Vue 控制台 | `vps/fanout/frontend/` | 页面、轮询、表单、主题 |
| Python 工具箱 | `src/vpngate_toolbox/` | CSV 获取/解析、候选池、画像、周期调度 |
| 安装器 | `vps/deploy/` | 依赖、完整包、systemd、备份、恢复、卸载 |

## 启动顺序

`runFanout` 按固定阶段启动；每个阶段只依赖前一个阶段的产物：

```text
initStructuredLogging
loadEnvFile(intelligence.env)
parseBootConfig          # flag 解析
validateBootConfig       # 容量、FANOUT_PUBLIC_IP 回退
  -version / -check-state 可在此退出
bootstrapHost            # root、目录、主 netns、实例锁、实例标识、
                         # 配置事务恢复、公网 IP、host 准备、面板
loadWebSettings          # 端口/监听地址/来源过滤
initControl              # Manager → 候选拉取 → Console → 状态恢复 →
                         # 首装 Slot → 内置接入恢复
startBackground          # watch/automation/checks/sstp/health/pool
installShutdownHandler   # SIGINT/SIGTERM 清理
initHTTP                 # 路由、鉴权、访问路径、中间件、server
serveUntilSignal
```

回归由 `boot_test.go` 覆盖；阶段和退出码的约束见 [ADR-0009](../adr/0009-boot-phases.md)。

### 启动时的关键动作

- **实例锁**：同一工作目录只允许一个实例，防止两份进程共用 `state.json`。
- **主 netns 记录**：必须在创建任何隧道之前记下母机网络命名空间，否则线程可能被带进隧道。
- **配置事务恢复**：管理设置、Slot 状态、内置配置、出口计划各自有事务日志；启动时先恢复未完成事务。
- **状态恢复**：按 `state.json` 重建隧道；节点可能已从候选池消失，用存盘配置兜底。
- **首装引导**：`FANOUT_INITIAL_REGION`/`FANOUT_INITIAL_NODE` 只在无隧道时创建 slot 1；常规数量由控制台管理。
- **内置接入恢复**：即使没有 VPN Gate Slot，也要恢复直出和第三方入口。

## 控制面

### Manager

`Manager` 维护所有 `Tunnel`：

- `tunnels map[int]*Tunnel`：Slot 编号 → 隧道。
- `nodes []Node`：当前候选池。
- `maxSlots`：容量上限。
- `connectBudget`：检测和正式建连共享的并发预算。
- `observations`/`observationStats`：延迟、失败率、排序证据。
- `diagnostics`：临时检测租约，避免和正式出口抢同一节点。
- `rotationCh`/`rotationWorker`：换节点请求串行执行。

### Console

`Console` 维护用户意图和控制台状态：

- `ExitPlan`：目标数量、策略、REALITY 目标、待删除 Slot。
- `check`/`results`：检测任务和结果。
- `automation`：刷新/检测周期。
- `refreshState`：来源刷新状态。
- `sstp`：SSTP 设置和结果。
- `third`：第三方代理。
- `version`：控制台可见状态的单调版本号。

`Console` 每 15 秒对账一次，并在候选池变化时唤醒；换节点 worker 在状态恢复前启动。

### Native

`Native` 是唯一的内置 Xray 后端：

- `native.json` 保存入站、客户端、绑定、加密参数。
- 每次变更整份重写 Xray 配置，不做增量修改。
- 配置运行失败时保持阻断，不静默回退到直出。
- 支持 VLESS 的传输/安全组合、REALITY、VLESS Encryption。
- `native.previous.json` 用于回滚。

### 第三方与 SSTP

- `ThirdPartyManager` 保存外部 SOCKS5，生成对应的 VLESS 入站；凭据只在私有状态。
- `SstpManager` 检测官方 SSTP 端点，导出 `sstp://` 或 JSON；与 OpenVPN 证据隔离。

## 数据面

### 出口

每条 Slot：

```text
host netns
  └─ veth pair
       └─ slot netns
            ├─ tun0（OpenVPN）
            ├─ 路由/DNS
            └─ OpenVPN 进程
host
  └─ 127.0.0.1:<port> SOCKS5 → dial in slot netns
```

- 内部 SOCKS5 只监听 `127.0.0.1`，供内置 Xray 使用。
- 公网 SOCKS5 是独立的 Xray 入站，只有显式开启才存在。
- 每条隧道有 `workCtx`、`opMu`、`changeMu`；停止/删除会取消上下文并清理 netns、veth、iptables。
- kill switch 阻止隧道断开后流量退回公网。

### 路由

```text
客户端 → VLESS 入站 → 出站
                         ├─ Slot SOCKS5（绑定出口）
                         ├─ 第三方 SOCKS5（固定上游）
                         └─ 直出（本机网络）
```

入站绑定到 Slot 时，Xray 生成对应出站和路由规则；未绑定入站阻断。换节点时入站重新绑定，端口和客户端凭据不变。

## 持久化

### JSON 意图

| 文件 | 内容 | 语义 |
| --- | --- | --- |
| `state.json` | 出口、端口、节点、凭据、策略、停止意图 | fail-closed |
| `console.json` | 目标数量、策略、REALITY 目标、待删除 Slot | fail-closed |
| `native.json` | 入站、客户端、绑定、加密参数 | fail-closed |
| `settings.json` | 管理端口、监听、来源过滤、订阅 token | 事务提交 |
| `password`/`basepath` | 管理身份 | 独立文件 |

意图类文件读不懂或版本过新时拒绝启动/保留原文件，不猜测。每个写路径走配置事务（见 [ADR-0003](../adr/0003-config-transactions.md)）。

### SQLite

SQLite 只收**可再生的高频数据**：

- 检测结果（`checks`）
- 出口观察快照（`slot_health`）
- IP 类型缓存（`ip_type`）

意图类留在 JSON，因为 Python 安装器/验收脚本直接读 JSON。SQLite 打不开时检测数据回退旧 JSON（fail-open）；意图类不适用。见 [ADR-0007](../adr/0007-sqlite-scope.md)。

## 并发与锁

| 锁 | 保护 |
| --- | --- |
| `Manager.mu` | 隧道 map、候选、诊断租约 |
| `Tunnel.mu` | 状态、节点、凭据、策略、延迟 |
| `Tunnel.opMu` | 建连/换节点/停止等操作 |
| `Tunnel.changeMu` | 换节点与策略变更串行 |
| `Console.targetMu` | 出口计划 |
| `Console.checksMu` | 检测任务和结果 |
| `Console.refreshMu` | 来源刷新 |
| `saveMu` | 状态落盘与关闭 |

约定：

- 先落盘意图，再改运行态。
- 锁顺序固定；不要在持锁时调用会反向取锁的方法。
- 换节点只入队，由单 worker 串行执行；`publishUp` 不直接回调换节点。
- 停止/删除要取消 `workCtx`，并允许从任意非 stopped 状态进入 stopped。

## 为什么这样设计

- [ADR-0001：Slot 生命周期](../adr/0001-slot-lifecycle.md)
- [ADR-0002：证据链](../adr/0002-evidence-chain.md)
- [ADR-0003：配置事务](../adr/0003-config-transactions.md)
- [ADR-0004：订阅格式](../adr/0004-subscription-format.md)
- [ADR-0005：更新事务](../adr/0005-update-transaction.md)
- [ADR-0006：Xray 重载](../adr/0006-xray-reload.md)
- [ADR-0007：SQLite 范围](../adr/0007-sqlite-scope.md)
- [ADR-0008：Slot 状态机](../adr/0008-slot-state-machine.md)
- [ADR-0009：启动阶段](../adr/0009-boot-phases.md)

## 改代码先看哪里

| 要改什么 | 先看 |
| --- | --- |
| 出口数量/策略 | `console.go`、`console_defaults.go`、`policy.go` |
| 出口建连/换节点 | `manager_lifecycle.go`、`tunnel*.go`、`rotation.go` |
| 健康检查 | `health.go`、`connect_observations.go` |
| 订阅 | `sub.go`、`sub_clash.go` |
| 入站/客户端 | `native_inbounds.go`、`native_build.go`、`native_security.go` |
| 检测/证据 | `console_checks.go`、`console_ttl.go`、`egress.go`、`ip_type_cache.go` |
| 来源/刷新 | `pool_refresh.go`、`console_automation.go`、`src/vpngate_toolbox/` |
| 安装/更新/卸载 | `vps/deploy/`、`vps/install.sh` |
| 前端 | `vps/fanout/frontend/` |

## 相关

- [后端](backend.md)
- [前端](frontend.md)
- [安装器](installer.md)
- [测试](testing.md)
- [ADR](../adr/README.md)
