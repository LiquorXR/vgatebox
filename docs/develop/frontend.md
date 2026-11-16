# 前端

控制台是 `vps/fanout/frontend` 下的 Vue 3 单页应用，构建产物写入 `vps/fanout/webdist`，由 Go 通过 `embed` 打进同一个二进制。没有独立前端服务，也没有运行时 CDN。

## 技术栈

| 组件 | 版本/用途 |
| --- | --- |
| Vue | 3.5 |
| TypeScript | 5.9 |
| Vite | 7 |
| Tailwind CSS | 4（CSS-first tokens） |
| TanStack Vue Query | 5（轮询、缓存、失焦暂停） |
| Reka UI | 交互原语（shadcn-vue 风格源码组件） |
| Zod | 表单校验 |
| Lucide | 图标 |
| Vitest + happy-dom | 单元/页面集成测试 |
| Playwright | 浏览器 E2E（真实后端，Linux） |

不引入运行时 CDN、外部字体、图表库或图标字体。二维码由服务端 `go-qrcode` 生成。

## 目录

```text
vps/fanout/frontend/
  index.html                 控制台入口
  login.html                 登录入口
  src/
    api/
      client.ts              typed fetch、错误处理、URL 前缀
      queries.ts             查询键与轮询
      types.ts               接口类型
    components/              AppShell、导航、表格、对话框、Toast
    lib/                     状态词表、格式化、订阅 URL、主题、策略默认值、倒计时
    login/                   登录页
    pages/                   总览、出口、节点池、订阅、接入、设置
    pages/access/            接入 tab、入站新增/详情
    pages/exit/              出口目标/详情
    pages/nodes/             节点筛选/表格/检测/侧栏/来源/周期/详情
    styles/                  设计 token 与基础样式
  vite.config.ts             多入口、base './'、输出 webdist
  webdist/.gitkeep           保证未构建时 Go 仍可编译
```

## 构建

```sh
cd vps/fanout/frontend
npm ci --ignore-scripts
npm run typecheck
npm run test:unit
npm run build
```

然后构建 Go 程序：

```sh
cd vps/fanout
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -o ../../dist/vgatebox-manager .
```

CI 会先安装 Node 22、跑 `npm ci --ignore-scripts` 和 `npm run build`，再跑 Go 测试和打包。`scripts/build_vps_package.py` 在缺少 `webdist/index.html` 时直接失败，避免发布没有前端的二进制。

`webdist` 构建产物不提交，只保留 `.gitkeep`。新克隆直接 `go test` 可以编译；未构建时访问 `/` 会看到明确的构建提示，而不是空白页。

## 路由

前端使用 **hash 路由**（`#/exits`），不要求 Go 增加 SPA fallback，也不会因随机访问路径刷新 404。

| 路由 | 页面 |
| --- | --- |
| `#/` | 总览 |
| `#/exits` | 出口 |
| `#/nodes` | 节点池 |
| `#/subscription` | 订阅 |
| `#/access` | 接入 |
| `#/settings` | 设置 |
| 其他 | 重定向到 `#/` |

资源用相对路径；根路径使用 `/assets/...`，`/next/` 别名使用 `/next/assets/...`，两者指向同一份构建产物。API 前缀由 `src/api/client.ts` 根据当前 `location.pathname` 推导，例如 `/aB3/` 或 `/aB3/next/` → `/aB3/`。

## 轮询

| 页面/数据 | 频率 | 说明 |
| --- | --- | --- |
| `console/version` | 5 秒（运行中 2 秒） | 小响应；变化或 60 秒兜底才拉全量 `console` |
| `exits` | 5 秒 | 出口和入站视图 |
| `nodes` | 20 秒 | 只在页面挂载时请求 |
| 订阅地址 | 页面加载一次 | 不在后台轮询 |
| 接入 | 按 tab 启用 | 切到哪个 tab 请求哪个接口 |

窗口失焦时 TanStack Query 停止后台轮询；回到前台再恢复。`/api/console` 支持 ETag/304，但当前前端仍走版本轮询。

## API 客户端

- `client.ts` 负责：同源 Cookie、`Cache-Control: no-store`、JSON 解析、错误消息、随机路径前缀。
- `queries.ts` 定义查询键和轮询策略；页面通过组合式函数使用。
- `types.ts` 是接口类型的手写镜像；后端字段变更时同步更新。
- 写操作使用 `post`；失败显示 Toast，不把错误吞掉。
- 登录态是 Cookie；开发服务器与代理必须同源。

## 开发

```sh
cd vps/fanout/frontend
npm run dev
```

Vite 默认把 `/api`、`/sub`、`/login` 代理到 `http://127.0.0.1:8899`。如果本机管理实例使用随机访问路径，可以通过 `VGATEBOX_API_TARGET` 指向带路径的完整地址，或在开发时临时使用无前缀路径。

## 设计系统

- Tailwind CSS 4 的 CSS-first tokens 在 `src/styles/tokens.css`。
- 组件是源码级 shadcn-vue 风格，交互原语用 Reka UI；没有运行时组件库 CDN。
- 主题通过 `theme-init.ts` 在首屏前应用，支持浅色/深色/跟随系统。
- 状态词表、颜色和标签集中在 `lib/status.ts`、`lib/format.ts`，页面不要各自硬编码。
- 移动端使用底部导航；页面内容需要横向滚动时放在自己的 `overflow-x-auto` 容器里。

## 安全

- 管理页 HTML：`Cache-Control: no-store`，严格 CSP（`script-src 'self'`）。
- 管理 API JSON：`no-store`，避免凭据/订阅 token 进入浏览器缓存。
- 状态变更只接受 POST；`Auth.Wrap` 拒绝浏览器标记为 `Sec-Fetch-Site: cross-site` 的写请求。
- 通过 HTTPS 访问时 Cookie 带 `Secure`，响应带 HSTS。
- `/assets/`、`/next/assets/` 和 favicon 在登录前可读；它们不含管理数据。
- 登录页支持无 JS 原生表单提交；成功 303 回来源页面，XHR/fetch 仍返回 JSON。

## 测试

```sh
npm run typecheck
npm run test:unit
npm run build
```

Vitest 覆盖：

- 纯逻辑：状态词表、格式化、订阅 URL、主题、策略默认值、出口倒计时、接入错误、入站命名；
- API 客户端与 payload 契约；
- 共享组件（复制、对话框、延迟趋势、状态徽章）；
- 登录页（错误提示、空密码拦截）；
- 页面集成：根路径与导航、出口目标/地区库存、策略/换 IP、删除自动出口与归档清理、SOCKS5 凭据、入口复制、订阅格式/二维码、节点筛选、接入 tab、主题菜单、移动端导航。

E2E 用 Playwright 跑真实后端（Linux，需 root/netns/iptables），覆盖登录→总览、设数量→等待出口、订阅复制/二维码、删出口回 0、移动端导航与接入页签。详见 [测试](testing.md)。

happy-dom 没有布局引擎；移动端横向溢出仍要在真实浏览器/设备验收。

## 加一个页面/查询

1. 在 `src/pages/` 新建页面，路由加在 `router.ts`，导航加在 `lib/nav.ts`。
2. 在 `api/types.ts` 加类型，`api/queries.ts` 加查询键/轮询。
3. 用现有 `PageHeader`、`Panel`、`EmptyState`、`StatusBadge`、`UiButton` 等组件，保持风格一致。
4. 错误用 `toastError`/`useAccessError`，不要静默失败。
5. 加 Vitest 页面集成测试；需要真实后端的流程放 E2E。
6. 更新 [产品与概念](../start/concepts.md) 或对应使用页面的文案；必要时补截图。

## 当前边界

- 页面集成测试使用 mock API，不替代真实 VPS 的隧道、防火墙、SSTP 和第三方代理验收。
- 慢网、移动端横向溢出、真实浏览器主题切换需要真实环境验收。
- 开启外部 SOCKS5 和保存第三方代理会重载 Xray；反向代理读超时至少 120 秒，否则可能 502。
- `/advanced` 只做 302 跳转到 `/#/access`，不再提供独立页面。

## 相关

- [架构](architecture.md)
- [后端](backend.md)
- [测试](testing.md)
- [安全](../operate/security.md)
- [HTTP API](../reference/api.md)
