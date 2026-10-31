# 新控制台（前端）

新控制台是 `vps/fanout/frontend` 下的 Vue 3 单页应用，构建产物写入 `vps/fanout/webdist`，由 Go 通过 `embed` 打进同一个二进制。部署方式、端口和访问路径不变。

当前状态：

- `/`：新控制台，默认入口。
- `/next/`：新控制台的别名，保留给已收藏 `/next/` 的浏览器。
- `/advanced`：旧地址会重定向到新控制台的「接入 → 高级接入」，旧页面已删除。

主控制台旧页面 `console.html` 和旧登录页已经删除。部署后按真实 VPS 验收清单测试；验收重点见文末「当前边界」。

## 技术栈

- Vue 3 + TypeScript + Vite 7
- Tailwind CSS 4（CSS-first tokens，`src/styles/tokens.css`）
- shadcn-vue 风格的源码组件，交互原语使用 Reka UI
- TanStack Vue Query：轮询、缓存、窗口失焦暂停
- Zod：第三方代理表单校验
- Lucide 图标
- Vitest：纯逻辑与回归测试

不引入运行时 CDN、外部字体、图表库或图标字体。二维码仍由服务端 `go-qrcode` 生成。

## 目录

```text
vps/fanout/frontend/
  index.html            控制台入口
  login.html            登录入口
  src/
    api/                typed client、查询键、接口类型
    components/         AppShell、导航、表格、对话框、Toast
    lib/                状态词表、格式化、订阅 URL、主题、计划默认值
    login/              登录页
    pages/              总览、出口、节点池、订阅、接入、设置
    styles/             设计 token 与基础样式
  vite.config.ts        多入口、base './'、输出到 webdist
  webdist/.gitkeep      保证未构建时 Go 仍可编译
```

## 构建

```sh
cd vps/fanout/frontend
npm ci --ignore-scripts
npm run typecheck
npm run test:unit
npm run build
```

随后正常构建 Go 程序：

```sh
cd vps/fanout
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -o ../../dist/vgatebox-manager .
```

CI 的 `VPS tests and package` 会先安装 Node 22、执行 `npm ci --ignore-scripts` 和 `npm run build`，再跑 Go 测试和打包。`scripts/build_vps_package.py` 在缺少 `webdist/index.html` 时会直接失败，避免发布没有前端的二进制。

`webdist` 的构建产物不提交；只保留 `.gitkeep`。因此新克隆直接 `go test` 可以编译，未构建时访问 `/next/` 会看到明确的构建提示，而不是空白页。

## 本地开发

```sh
cd vps/fanout/frontend
npm run dev
```

Vite 默认把 `/api`、`/sub`、`/login` 代理到 `http://127.0.0.1:8899`。如果本机管理实例使用随机访问路径，可以通过 `VGATEBOX_API_TARGET` 指向带路径的完整地址，或在开发时临时使用无前缀路径。登录态是 Cookie，开发服务器与代理必须保持同源。

## 路由与访问路径

- 前端使用 hash 路由（`#/exits`），不要求 Go 增加 SPA fallback，也不会因随机访问路径刷新 404。
- 资源用相对路径；根路径使用 `/assets/...`，`/next/` 别名使用 `/next/assets/...`，两者指向同一份构建产物。
- API 前缀由 `src/api/client.ts` 根据当前 `location.pathname` 推导，例如 `/aB3/` 或 `/aB3/next/` → `/aB3/`。

## 服务端行为

- 新 UI 的 HTML：`Cache-Control: no-store`，严格 CSP（`script-src 'self'`）。
- `/assets/`、`/next/assets/` 和 favicon 在登录前可读，登录页自身的 CSS/JS 不依赖会话；它们不包含管理数据。
- 带指纹的静态资源：一年 `immutable`；`favicon.svg` 等稳定名称使用 `no-cache`。
- 文本响应按 `Accept-Encoding` 做 gzip；图片和字体跳过。
- `/advanced` 仅做 302 跳转到 `/#/access`，不再提供独立 HTML；所有页面都使用新控制台。
- 登录页支持无 JS 的原生表单提交：成功时 303 回到来源页面；XHR/fetch 仍返回 JSON。

## API 使用

新页面复用现有接口，不改变订阅格式和出口语义。轮询按页面拆分：

- 总览 / 出口：`console` 5 秒、`exits` 5 秒。
- 节点池：`nodes` 20 秒，只在页面挂载时请求。
- 订阅：订阅地址只在页面加载时请求。
- 接入：按 tab 启用对应接口。
- 窗口失焦时 TanStack Query 停止后台轮询。

后续可把 `console` 改为 ETag 或 SSE；当前先保持接口兼容。

## 测试

```sh
cd vps/fanout/frontend
npm run typecheck
npm run test:unit
npm run build
npx playwright install chromium
npm run test:e2e
```

Vitest 覆盖纯逻辑、API 客户端、主题、状态/订阅/自动化格式化、共享组件和登录页；Playwright 覆盖根路径、出口目标弹窗、订阅格式/二维码、节点筛选、接入页签、主题持久化、移动端无横向溢出和登录错误。旧的 `tests/test_console_*.mjs` 已随旧控制台删除。

## 已完成切换

1. 主控制台页面、登录页和设置页已迁移到 Vue。
2. `/` 已指向新控制台，`/next/` 保留为别名。
3. 旧 `console.html`、旧 `login.html` 和旧控制台 Node 测试已删除。
4. 入站 CRUD、客户端和导出已并入「接入 → 高级接入」；`/advanced` 只保留重定向。

## 当前边界

- `/advanced` 只做重定向；旧页面和样式已删除。
- 候选列表仍由 `/api/nodes?all=1` 全量返回；前端分页，后续应在服务端分页或虚拟滚动。
- `console` 仍是 5 秒轮询；ETag、SSE 和凭据按需加载尚未实现。
- Playwright 使用 mock API 覆盖主流程，不替代真实 VPS 的隧道、防火墙、SSTP 和第三方代理验收。
- 需要在真实 VPS 上验收登录、随机路径、移动端、慢网、出口操作、SSTP 和第三方代理。
- 开启外部 SOCKS5 和保存第三方代理会重载 Xray。若通过反向代理访问，请让代理读超时覆盖重载时间（建议至少 120 秒），否则可能收到 502；服务端现在会把 5xx 和 panic 写入日志，前端会刷新状态而不是停在错误上。
