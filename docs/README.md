# vgatebox 文档

本文档对应 `main` 分支上的当前版本，描述 vgatebox 的**现有行为**，不是版本变更记录。历史设计决策见 [ADR](adr/README.md)，版本变化见 GitHub Releases。

## 我想……

| 我想…… | 读这篇 |
| --- | --- |
| 第一次安装，尽快让客户端上网 | [快速开始](start/quickstart.md) |
| 理解候选、出口、接入、订阅之间的关系 | [产品与概念](start/concepts.md) |
| 知道这个项目适不适合我 | [产品概览](start/overview.md) |
| 调整出口数量、国家、IP 类型，或换 IP | [出口](use/exits.md) |
| 用 VPS 本机网络、VPS SOCKS5 或自己的 SOCKS5 | [接入](use/access.md) |
| 新建或管理 VLESS 入站 | [高级接入](use/inbounds.md) |
| 用 v2rayN 或 Clash 导入订阅 | [订阅](use/subscription.md) |
| 刷新候选、检测节点、看周期任务 | [节点池](use/nodes.md) |
| 导出原始 SSTP 资源 | [原始 SSTP](use/sstp.md) |
| 安装、升级、备份、卸载 | [安装](operate/install.md) · [更新](operate/update.md) · [卸载](operate/uninstall.md) |
| 加固公网管理入口 | [安全](operate/security.md) |
| 估算能跑多少出口、怎么调优 | [容量与资源](operate/capacity.md) |
| 出问题了，按症状排查 | [故障排查](operate/troubleshooting.md) |
| 查环境变量、路径、端口、权限 | [配置参考](reference/configuration.md) |
| 查命令行 | [CLI 参考](reference/cli.md) |
| 调 HTTP API 或写自动化 | [HTTP API](reference/api.md) |
| 看订阅链接和 Clash 字段 | [订阅格式](reference/subscription-format.md) |
| 看支持的系统、客户端和协议边界 | [边界与兼容](reference/limits.md) |
| 改代码、跑测试、发版本 | [架构](develop/architecture.md) · [测试](develop/testing.md) · [发布](develop/release.md) |
| 看为什么这样设计 | [ADR](adr/README.md) |

## 阅读路径

**第一次部署**

[产品概览](start/overview.md) → [快速开始](start/quickstart.md) → [产品与概念](start/concepts.md) → [订阅](use/subscription.md)

**日常使用**

[出口](use/exits.md) → [接入](use/access.md) → [节点池](use/nodes.md) → [故障排查](operate/troubleshooting.md)

**运维**

[安装](operate/install.md) → [安全](operate/security.md) → [容量与资源](operate/capacity.md) → [更新](operate/update.md) → [备份与恢复](operate/backup-restore.md)

**开发**

[架构](develop/architecture.md) → [后端](develop/backend.md) → [前端](develop/frontend.md) → [测试](develop/testing.md) → [贡献](develop/contributing.md)

## 文档约定

- **只写当前行为**：正文不写版本演进、外部面板迁移或重构任务编号；历史只进 ADR、CHANGELOG 和 Release Notes。
- **一页一个任务**：任务页先写读者能完成什么，再写前置条件、步骤、验证和失败处理。
- **单一事实源**：API 路由表、环境变量、CLI、路径等参考信息尽量由代码或契约文件校验；同一事实不在多页重复。
- **边界集中**：支持范围、协议限制、容量建议集中在 [边界与兼容](reference/limits.md) 和 [容量与资源](operate/capacity.md)，功能页只链接。
- **命令可复制**：命令块使用 `sh` 或 `powershell` 标注；占位符统一写 `<...>`。
- **默认不截图**：用精确的菜单路径和字段名；必须截图时放在 `docs/assets/`，并标注适用版本。

## 文档地图

```text
docs/
  README.md                    本页：阅读路径、约定、地图
  start/                       上手
    overview.md                产品是什么、解决什么问题、不做什么
    quickstart.md              从空 VPS 到客户端能上网
    concepts.md                候选/出口/接入/订阅/节点池/证据链/状态
  use/                         日常操作
    exits.md                   出口数量、策略、生命周期、换 IP、删除
    access.md                  本机网络、VPS SOCKS5、第三方 SOCKS5
    inbounds.md                高级接入：VLESS 入站、传输/安全组合、归档入口
    subscription.md            两种订阅格式、排序、token、二维码、客户端
    nodes.md                   来源刷新、检测、镜像后备、TTL、并发
    sstp.md                    原始 SSTP 导出与边界
  operate/                     部署与运维
    install.md                 系统要求、安装、首次登录、防火墙
    update.md                  检查/安装更新、失败恢复
    uninstall.md               卸载、保留/清除、兼容清理
    backup-restore.md          备份范围、权限、恢复
    security.md                暴露面、口令/路径、Cookie/CSP/HSTS、反代
    capacity.md                fd/内存/端口/并发、容量预检、调优
    troubleshooting.md         症状 → 检查 → 处理
  reference/                   参考手册
    cli.md                     vgatebox 与 toolbox 命令
    configuration.md           环境变量、配置文件、默认值
    api.md                     62 条路由、鉴权、稳定接口
    subscription-format.md     v2rayN/Clash 链接字段与限制
    state-and-files.md         目录、文件、权限、排障入口
    limits.md                  OS/客户端/协议矩阵与能力边界
  develop/                     开发
    architecture.md            组件、数据流、启动顺序、并发模型
    backend.md                 Manager/Console/Slot/存储/不变量
    frontend.md                Vue 应用、构建、路由、轮询
    installer.md               安装/更新/卸载事务
    testing.md                 测试矩阵、本地/CI、E2E
    release.md                 版本、打包、发布、回滚
    contributing.md            开发环境、代码风格、PR 清单
  adr/                         设计决策记录（历史）
    README.md                  ADR 索引与状态说明
    0001 ~ 0009                已接受的决策
```

## 维护

- 改路由、环境变量、CLI、安装路径或 UI 文案时，同一个 PR 更新对应文档。
- `scripts/check_docs.py` 检查断链、孤儿页、禁用词、API 路由漂移；PR 会跑这个脚本。
- 参考表以代码/契约为准：路由以 `vps/fanout/testdata/api/routes.json` 为准，路径以 `vps/fanout/internal/paths/paths.go` 为准。
- 每篇文档的“相关”链接是阅读入口；新增页面必须从本页或已有页面可达。
- 文档中的“已验证”指在仓库 CI 或真实 VPS 上实际跑过；未验证的内容必须明确标注。
