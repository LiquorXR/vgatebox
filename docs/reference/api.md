# HTTP API

vgatebox 的管理 API 由 Go 进程提供，默认监听 `8899`，所有路径都在随机访问路径之后：

```text
http://<VPS>:8899/<访问路径>/api/...
```

路由契约由 `vps/fanout/routes.go` 和 `testdata/api/routes.json` 锁定，`TestAPIContract` 校验三者一致；当前共 62 条路由。

## 鉴权

| 类型 | 适用 | 规则 |
| --- | --- | --- |
| `session` | 大多数 `/api/*` | 需要 `fanout_session` 登录 Cookie；未登录 API 返回 401 |
| `mixed` | `/` | 页面入口；未登录跳登录页 |
| `token` | `/sub` | 免登录，但校验独立订阅 token |
| `none` | `/healthz` | 免登录，只返回非敏感字段 |

登录：

```http
POST /<访问路径>/login
Content-Type: application/x-www-form-urlencoded

password=<管理密码>
```

成功返回 `Set-Cookie: fanout_session=...`，或对浏览器表单返回 303。登录失败 8 次/2 分钟会返回 429。

### 通用规则

- 状态变更接口只接受 `POST`（少数明确支持 `GET/POST` 或 `DELETE` 的路由除外）；其他方法返回 405。
- 浏览器标记为 `Sec-Fetch-Site: cross-site` 的写请求返回 403。
- 管理 API JSON 响应带 `Cache-Control: no-store`。
- 未登录访问 API 返回 401；未登录访问页面返回登录页。
- 请求体有上限：登录 8 KB、设置 32 KB、控制台 JSON 8 MB；超限返回 4xx。
- 错误统一为 `{"error":"..."}`；成功通常是 `{"ok":true}` 或具体对象。
- 异步任务返回 202 和任务视图；冲突/未就绪返回 409。

## 稳定接口

以下接口面向外部集成，字段相对稳定；其余 `/api/*` 主要是控制台内部使用，随 UI 演进可能调整。

| 接口 | 用途 |
| --- | --- |
| `GET /sub?token=...` | 订阅正文（Base64/明文/Clash） |
| `GET /healthz` | 免登录探活 |
| `GET /api/metrics` | Prometheus 文本指标（需登录） |
| `GET /api/console/version` | 控制台可见状态版本号（需登录） |
| `GET /api/console` | 控制台摘要（需登录，建议配合版本轮询） |

## 路由表

### 页面与公开端点

| 方法 | 路径 | 鉴权 | 说明 |
| --- | --- | --- | --- |
| GET/HEAD | `/` | mixed | 控制台首页；未登录显示登录页 |
| GET/HEAD | `/healthz` | none | 版本、uptime、Slot 计数、候选池龄、面板状态 |
| GET | `/sub` | token | 订阅正文；参数见 [订阅格式](subscription-format.md) |
| GET | `/api/sub` | session | 当前订阅地址/token（管理页用） |
| POST | `/api/sub/reset` | session | 重置订阅 token，旧地址立即失效 |

### 控制台摘要与轮询

| 方法 | 路径 | 鉴权 | 说明 |
| --- | --- | --- | --- |
| GET | `/api/console` | session | 目标/已就绪/等待/候选/检测/刷新/自动化/地区/内存等摘要；支持 ETag/304 |
| GET | `/api/console/version` | session | 单调版本号；变化时再拉全量 |
| GET | `/api/console/qr` | session | 订阅二维码 PNG；参数同 `/sub` |
| GET | `/api/metrics` | session | Prometheus 文本格式指标 |
| GET | `/api/exits` | session | 出口 + 入站 join 视图 |
| GET | `/api/nodes` | session | 候选列表；`limit`/`offset` 分页；不带 `limit` 返回全量 |
| GET | `/api/regions` | session | 地区库存统计（兼容接口） |

### 出口/Slot

| 方法 | 路径 | 鉴权 | 说明 |
| --- | --- | --- | --- |
| POST | `/api/console/target` | session | 应用出口计划（数量/策略/REALITY 目标） |
| POST | `/api/console/slot/delete` | session | 删除指定自动出口并把目标数减 1；可清理归档入口 |
| POST | `/api/slot/delete` | session | 兼容的 Slot 删除（不更新目标数；新 UI 不用） |
| POST | `/api/slot/resume` | session | 恢复已停止出口 |
| GET/POST | `/api/slot/policy` | session | 读取/设置某出口策略 |
| POST | `/api/slot/policy/rollback` | session | 回滚到上一份策略 |
| POST | `/api/stop` | session | 停止指定出口 |
| POST | `/api/swap` | session | 就地更换出口节点，端口不变 |
| POST | `/api/cred` | session | 设置/随机重置出口 SOCKS5 凭据 |
| POST | `/api/provision` | session | 批量创建出口（兼容/内部） |
| GET | `/api/jobs` | session | 任务列表 |
| POST | `/api/jobs/dismiss` | session | 关闭任务 |
| GET | `/api/export/xray` | session | 导出 Xray 配置 |
| GET/POST | `/api/config/rollback` | session | 查询/回滚内置入站配置 |

### 节点池与检测

| 方法 | 路径 | 鉴权 | 说明 |
| --- | --- | --- | --- |
| POST | `/api/console/refresh` | session | 手动刷新候选；取消正在运行的检测 |
| GET/POST | `/api/console/source-settings` | session | 来源等待、镜像开关/最大年龄 |
| GET/POST | `/api/console/automation` | session | 自动刷新开关/间隔 |
| GET/POST | `/api/console/check/settings` | session | 检测并发、IP 类型开关、TTL |
| POST | `/api/console/check` | session | 启动候选/出口检测 |
| POST | `/api/console/check/cancel` | session | 取消当前检测 |
| GET/POST/DELETE | `/api/console/ip-type-cache` | session | 查看/清除 IP 类型缓存 |

### 接入与内置入站

| 方法 | 路径 | 鉴权 | 说明 |
| --- | --- | --- | --- |
| POST | `/api/console/direct` | session | 启用/停用本机网络 |
| GET/POST | `/api/console/socks` | session | 查看/开启某出口的公网 SOCKS5 |
| GET | `/api/xui/inbounds` | session | 入站列表（内置 Xray） |
| GET | `/api/xui/detail` | session | 入站详情 + 客户端 + 链接 |
| GET | `/api/xui/links` | session | 指定入站的分享链接 |
| POST | `/api/xui/bind` | session | 绑定入站到出口 |
| POST | `/api/xui/clone` | session | 以模板克隆到所有已就绪出口 |
| POST | `/api/xui/delete` | session | 删除入站 |
| POST | `/api/xui/encryption/rotate` | session | 重新生成 VLESS Encryption 参数 |
| POST | `/api/xui/encryption/set` | session | 打开/关闭 VLESS Encryption |
| POST | `/api/panel/inbound/new` | session | 新建 VLESS 入站 |
| POST | `/api/panel/inbound/update` | session | 修改入站端口/备注/启停 |
| POST | `/api/panel/client/add` | session | 新增客户端 |
| POST | `/api/panel/client/del` | session | 删除客户端（不能删最后一个） |
| POST | `/api/panel/client/reset` | session | 重置客户端凭据，旧链接失效 |

### 第三方 SOCKS5

| 方法 | 路径 | 鉴权 | 说明 |
| --- | --- | --- | --- |
| GET/POST | `/api/resources/third-party` | session | 列表/新增/编辑代理 |
| POST | `/api/resources/third-party/check` | session | 检测代理 |
| POST | `/api/resources/third-party/toggle` | session | 启用/停用/加入订阅 |
| POST | `/api/resources/third-party/subscribe` | session | 切换“加入订阅” |
| POST | `/api/resources/third-party/delete` | session | 删除代理及其接入入口 |

### 原始 SSTP

| 方法 | 路径 | 鉴权 | 说明 |
| --- | --- | --- | --- |
| GET | `/api/resources/sstp` | session | 候选、设置、检测结果 |
| POST | `/api/resources/sstp/settings` | session | 并发/周期/有效期/超时 |
| POST | `/api/resources/sstp/check` | session | 启动检测 |
| POST | `/api/resources/sstp/cancel` | session | 取消检测 |
| GET | `/api/resources/sstp/export` | session | 导出 `format=text|json` |

### 设置与更新

| 方法 | 路径 | 鉴权 | 说明 |
| --- | --- | --- | --- |
| GET/POST | `/api/settings` | session | 管理端口/监听/密码/路径/来源过滤 |
| GET | `/api/update/check` | session | 检查最新正式版 |
| POST | `/api/update/apply` | session | 启动在线更新 |
| GET | `/api/update/status` | session | 最近一次更新任务 |

## 示例

### 探活

```sh
curl -fsS http://127.0.0.1:8899/healthz
```

```json
{
  "status": "ok",
  "version": "v1.5.7",
  "uptime_seconds": 3600,
  "slots": { "total": 6, "up": 5 },
  "pool": { "candidates": 123, "read_at": "2026-01-01T00:00:00Z", "generated_at": "...", "age_seconds": 900 },
  "panel": { "available": true, "kind": "native" }
}
```

### 版本轮询

```sh
curl -b cookie.txt http://127.0.0.1:8899/<路径>/api/console/version
```

```json
{"version": 42}
```

前端先轮询这个接口；变化时再拉 `/api/console`，并用 60 秒兜底。`/api/console` 支持 `ETag` 和 `If-None-Match`，命中返回 304。

### 应用出口计划

```sh
curl -b cookie.txt -X POST http://127.0.0.1:8899/<路径>/api/console/target \
  -H 'Content-Type: application/json' \
  -d '{
    "count": 6,
    "policy": {
      "country": "JP",
      "allowed_ip_types": ["strict_residential", "unknown"],
      "maximum_latency_ms": 1500
    },
    "server_name": "www.tesla.com"
  }'
```

### 刷新候选

```sh
curl -b cookie.txt -X POST http://127.0.0.1:8899/<路径>/api/console/refresh
```

### 启动检测

```sh
curl -b cookie.txt -X POST http://127.0.0.1:8899/<路径>/api/console/check \
  -H 'Content-Type: application/json' \
  -d '{"kind":"checks","all":true}'
```

`kind` 可为 `checks`、`candidates`、`exits`；`hosts`/`slots` 选范围；`parallelism` 覆盖本次并发；`priority` 允许加入正在运行的任务。

### 订阅

```sh
curl -fsS 'http://127.0.0.1:8899/<路径>/sub?token=<token>&target=links'
```

### 指标

```sh
curl -fsS -b cookie.txt http://127.0.0.1:8899/<路径>/api/metrics
```

指标包括：

```text
vgatebox_slots_up
vgatebox_slots_degraded
vgatebox_slots_blocked
vgatebox_slots_total
vgatebox_pool_age_seconds
vgatebox_xray_restarts_total
vgatebox_check_last_total
vgatebox_check_last_passed
vgatebox_http_requests_total{path,status}
```

## 兼容与限制

- `/api/slot/delete` 保留给旧客户端；新界面使用 `/api/console/slot/delete`。
- `/api/regions` 保留给外部调用；新界面读 `/api/console` 的 `regions`。
- `/api/xui/*` 是内置 Xray 的入站管理接口，名称只是历史兼容，不表示支持 3x-ui。
- 管理 API 不是通用公开 API；没有版本前缀，字段可能随控制台变化。集成方应优先使用上表的稳定接口。
- 所有 API 都在随机访问路径之后；路径不是固定 `/`。
- 订阅 token 和登录密码不是同一个；API 需要登录 Cookie，订阅需要 token。

## 相关

- [订阅格式](subscription-format.md)
- [CLI 参考](cli.md)
- [配置参考](configuration.md)
- [产品与概念](../start/concepts.md)
- [安全](../operate/security.md)
