# 贡献

欢迎修 bug、补测试、改文档和提功能。vgatebox 的代码不复杂，但涉及 root、network namespace、iptables 和长期运行的隧道；改动要优先保证**不泄露流量、不静默回退、不破坏已分发配置**。

## 开发环境

| 工具 | 版本 |
| --- | --- |
| Go | 1.26.11 |
| Node | 22.12+ |
| Python | 3.9+（工具箱）；3.12（CI） |
| Linux | 完整 Go 测试需要 root、netns、iptables、openvpn |

系统依赖（Debian/Ubuntu）：

```sh
sudo apt-get install -y iproute2 iptables openvpn sstp-client ppp pkg-config
```

Windows/macOS 只能做交叉编译和前端/Python 测试；完整 Go 测试用 WSL2 或 CI。

## 仓库结构

```text
src/vpngate_toolbox/        Python 候选池/画像
vps/fanout/                 Go 管理进程
vps/fanout/frontend/        Vue 控制台
vps/deploy/                 安装/卸载/systemd/CLI
vps/install.sh              安装入口
tests/                      Python/安装/发行版测试
scripts/                    构建、检查、验收
docs/                       文档
config/                     Xray/候选默认配置
```

## 本地门禁

```sh
sh scripts/check.sh
```

等价于：

```sh
cd vps/fanout && GOOS=linux go vet ./... && GOOS=linux go test -c -o /tmp/check .
# Linux 上再加 go test ./... 和 python3 -m unittest discover -s tests -v
cd vps/fanout/frontend && npm run typecheck && npm run test:unit && npm run build
```

提交前至少跑：

- `gofmt`（Go 代码）
- `GOOS=linux go vet ./...`
- 相关 Go/Python/Vitest 测试
- `scripts/check_docs.py`（文档改动）
- `bash -n vps/install.sh vps/deploy/vgatebox`（脚本改动）

CI 还会跑 staticcheck、govulncheck、npm audit、E2E 和多发行版安装。

## 代码风格

### Go

- 错误文案以中文为主，便于界面直接展示；不要泄露 token/密码/URL。
- 不静默回退；失败要么阻断并记录原因，要么返回明确错误。
- 后台 worker 必须响应 `ctx.Done()`；停止/删除要清理 netns、veth、iptables、进程。
- 长操作不要持锁做 I/O；先取快照，释放锁后再执行。
- 新增日志用结构化字段（`slot`、`from`、`to`、`cause`），避免拼接敏感信息。
- 复杂逻辑注释“为什么”，不是“做了什么”。
- 路径统一走 `internal/paths`；不要自己拼固定文件名。

### Python

- 核心只用标准库；配置/解析错误抛 `ToolError`/`ServiceError`。
- 不打印 token、含 token 的 URL 或原始供应商响应。
- 外部请求加超时/大小限制；文件原子写、私密文件 0600。
- 测试放 `tests/test_*.py`，用 `unittest`。

### Vue/TypeScript

- 页面/组件/库分层；页面不要直接拼 API URL。
- 轮询和查询键放 `api/queries.ts`；类型放 `api/types.ts`。
- 状态词表/颜色/格式化放 `lib/`；不要在页面硬编码。
- 错误用 `toastError`/`useAccessError`，不要静默失败。
- 新页面加 Vitest 页面集成测试；真实后端流程放 Playwright。

## 提交与 PR

- 提交信息用现有风格：`fix:`、`feat:`、`perf:`、`refactor:`、`docs:`、`chore:`。
- 一个 PR 做一件事；大重构先开 issue/ADR。
- PR 描述写：问题、方案、影响面、测试命令、已知限制。
- 不提交：token、密码、订阅地址、二维码、真实节点凭据、备份、`node_modules`、`webdist`、`__pycache__`。
- 改路由/环境变量/CLI/安装路径/UI 文案时，同一个 PR 更新文档。

### PR 清单

- [ ] 代码/文档一致；用户文档不写版本演进、迁移或重构任务编号。
- [ ] 新增/修改行为有测试；失败路径有覆盖。
- [ ] `go vet`、相关 `go test`、Python/Vitest 通过。
- [ ] 路由变更更新 `routes.go` + `testdata/api/routes.json` + `docs/reference/api.md`。
- [ ] 环境变量/CLI/路径变更更新 `docs/reference/`。
- [ ] 订阅/协议变更更新 `docs/reference/subscription-format.md` 和 `limits.md`。
- [ ] 安全相关变更说明威胁模型和回滚方式。
- [ ] 没有泄露凭据/日志/截图。

## 文档规范

- 一页一个任务；先写读者能完成什么，再写前置条件、步骤、验证、失败处理。
- 只写当前行为；历史只进 ADR、CHANGELOG、Release Notes。
- 同一事实只保留一个权威页；其他页链接过去。
- 支持范围/协议限制集中到 `docs/reference/limits.md`。
- 命令可复制；占位符用 `<...>`；代码块标语言。
- 默认不截图；必须截图时放 `docs/assets/` 并标适用版本。
- 新增页面必须从 `docs/README.md` 或已有页面可达。
- 跑 `python3 scripts/check_docs.py`。

## ADR

设计决策记录放 `docs/adr/`：

```text
00NN-short-title.md
```

结构：

```markdown
# ADR-00NN：标题

- Status: proposed / accepted / superseded
- Date: YYYY-MM-DD
- 范围：涉及的代码/模块

## Context
## Decision
## Consequences
## Invariants
```

ADR 记录当时的决策和约束，不随后续实现改写；被取代时更新 Status 并链接新 ADR。

## 调试技巧

```sh
# 只读检查状态
/usr/local/bin/vgatebox-fanout -check-state -dir /var/lib/vgatebox/fanout

# 看结构化日志
journalctl -u vgatebox-fanout -f

# 看网络资源
ip netns list
ss -lntup
iptables -S

# 看 Xray/出口日志
tail -f /var/lib/vgatebox/fanout/run/xray.log
tail -f /var/lib/vgatebox/fanout/run/*.log
```

不要在生产 VPS 上直接跑会创建/删除 netns 的测试；用一次性容器或 CI。

## 安全

- 发现安全问题时不要公开贴 exploit/凭据；先私下联系维护者。
- 修改认证、订阅 token、代理凭据、更新校验、安装器时，说明攻击面和回滚。
- 不在日志/错误里输出 query、body、token、密码或供应商原始响应。

## 相关

- [架构](architecture.md)
- [后端](backend.md)
- [前端](frontend.md)
- [测试](testing.md)
- [发布](release.md)
- [文档首页](../README.md)
