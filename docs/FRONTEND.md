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
- Vitest：纯逻辑、组件与页面集成测试

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

## 反向代理

- 订阅地址和二维码优先使用 `FANOUT_PUBLIC_URL`（只填外部 origin，例如 `https://panel.example.com`）。
- 未显式设置时，只有在请求来自本机/私有网段或 `FANOUT_TRUSTED_PROXIES` 中配置的代理时，才接受 `Forwarded`、`X-Forwarded-Proto`、`X-Forwarded-Host`、`X-Forwarded-Port`；公网客户端伪造这些头不会改变生成的链接。
- 反向代理终止 TLS 时必须把 `X-Forwarded-Proto: https` 传给管理服务，并确保代理地址被识别为可信（本机/私有网段或 `FANOUT_TRUSTED_PROXIES`）。否则登录 Cookie 不会带 `Secure`，也不会发送 HSTS；`FANOUT_PUBLIC_URL` 不参与安全判断。
- 代理额外增加路径前缀时，应把该前缀配置为管理访问路径，而不是写进 `FANOUT_PUBLIC_URL`。
- 反向代理只代理管理页面时，节点链接优先使用 `FANOUT_PUBLIC_IP` 或自动探测到的公网 IPv4；探测不到时才回退到外部 origin 的主机名。

## 服务端行为

- 新 UI 的 HTML：`Cache-Control: no-store`，严格 CSP（`script-src 'self'`）。
- 管理 API 的 JSON 响应统一 `Cache-Control: no-store`，避免凭据、订阅 token 和客户端密钥进入浏览器缓存。
- 状态变更接口只接受 `POST`；其余方法返回 `405`。`Auth.Wrap` 还会拒绝浏览器标记为 `Sec-Fetch-Site: cross-site` 的写请求。
- 实际通过 HTTPS 访问（或可信代理转发 `X-Forwarded-Proto: https`）时，登录 Cookie 带 `Secure`，响应带 HSTS。
- 新建接入入口只支持 VLESS。VLESS 可选 TCP/RAW、XHTTP、gRPC、WebSocket、HTTPUpgrade、mKCP，并按官方矩阵限制 REALITY 与 TLS。
- 自动创建入口默认使用 VLESS + VLESS Encryption（X25519）+ RAW/TCP + REALITY + `xtls-rprx-vision`，REALITY 目标默认 `www.tesla.com`。
- VLESS 可单独启用 VLESS Encryption（X25519 或 ML-KEM-768）；启用后链接带 `encryption=`，`security=none` 也可用于公网。旧客户端可能无法解析。
- VLESS + mKCP 会进入 v2rayN/Base64 订阅，Mihomo 跳过。
- 升级时旧 VMess/Trojan/Hysteria2 入口（包括 VLESS over Hysteria2）会从 `native.json` 过滤，不再显示或进入订阅。
- `/assets/`、`/next/assets/` 和 favicon 在登录前可读，登录页自身的 CSS/JS 不依赖会话；它们不包含管理数据。
- 带指纹的静态资源：一年 `immutable`；`favicon.svg` 等稳定名称使用 `no-cache`。
- 文本响应按 `Accept-Encoding` 做 gzip；图片和字体跳过。
- `/advanced` 仅做 302 跳转到 `/#/access`，不再提供独立 HTML；所有页面都使用新控制台。
- 节点链接后端固定为内置 Xray。`3x-ui` 和 `xray-cf-lite` 不再作为外部入口后端；设置页没有后端切换，`/api/panel/mode` 已移除。`/api/xui/*` 保留为内置 Xray 的入站管理接口，名称只是历史兼容。
- 登录页支持无 JS 的原生表单提交：成功时 303 回到来源页面；XHR/fetch 仍返回 JSON。

## API 使用

新页面复用现有接口，不改变订阅格式和出口语义。轮询按页面拆分：

- 总览 / 出口：`console/version` 小版本 5 秒（运行中 2 秒），版本变化或 60 秒兜底时才拉全量 `console`；`exits` 5 秒。
- 节点池：`nodes` 20 秒，只在页面挂载时请求。
- 订阅：订阅地址只在页面加载时请求。
- 接入：按 tab 启用对应接口。
- 窗口失焦时 TanStack Query 停止后台轮询。

旧面板使用过、新控制台补充或合并的接口：

- `POST /api/cred`: body `{"slot": N, "user": "...", "pass": "..."}`；「接入 → VPS SOCKS5 → 详情」编辑凭据，两项都留空表示随机重置。凭据不再放在 URL 查询参数里。
- `POST /api/console/slot/delete`: body `{"slot": N, "cleanup_archive": false}`，删除指定自动出口并把控制台目标数量减 1，其他 Slot 编号不变。默认保留该出口的自动入口作为归档，之后重建同一编号时可以复用；`cleanup_archive: true` 会一并删除该 Slot 的自动归档入口，并在响应 `cleanup_error` 中报告清理失败。出口详情和「高级接入」的自动入口都使用该接口；旧 `/api/slot/delete` 只保留兼容，新 UI 不再调用。
- 对应出口已不存在的自动入口会在「高级接入」显示为「归档入口」。它不再有可删除的出口，使用「清理入口」调用 `POST /api/xui/delete?ids=N` 单独清理；手动入口和本机直出入口仍走各自的删除入口。
- `GET /api/console` 的 `regions` 字段：合并旧 `/api/regions` 的候选库存统计。出口目标弹窗显示各国家空闲节点数、最佳延迟和最佳速度，并明确标注这是候选库存而不是已验证出口。旧路由保留给外部调用。
- `GET /api/console/version`：控制台状态单调版本号（小响应）；前端先轮询它，变化或 60 秒兜底时才拉全量 `console`（应答自带 `console_version` 对齐）。需登录。
- `GET /healthz`：免登录探活，只返回版本/uptime/Slot 计数/候选池龄/面板状态等非敏感字段。
- `GET /api/metrics`：Prometheus 文本格式，需登录；含 Slot 计数、候选池龄、Xray 重启次数、上次检测数与按路径/状态的请求计数。建议 15 秒以上抓取。
- `POST /api/xui/clone?id=N`: 「高级接入 → 详情」以当前入口为模板，为所有已就绪出口创建入口。
- `POST /api/xui/encryption/rotate?id=N`: 为已启用 VLESS Encryption 的入口重新生成一对 `decryption`/`encryption` 参数；旧订阅链接立即失效。
- `POST /api/xui/encryption/set?id=N&enabled=1|0&key_type=x25519|mlkem768`: 打开或关闭一个 VLESS 入站的协议内加密层；打开时可按需生成新参数。

出口数量减少仍通过 `POST /api/console/target`。两个操作的区别：调整数量不指定删除哪个 Slot；删除自动出口会先固定要删除的编号，再减少目标数量。若仍有等待中的出口，删除后目标数仍可能由其他候选补位。

已移除与新入口重叠的旧路由：`/api/tunnels`（改用 `/api/exits`）、`/api/start` 和 `/api/replace`（改用 `/api/console/target` 与 `/api/swap`）、`/api/refresh`（改用 `/api/console/refresh`）、`/api/xui` 状态（后端固定为内置 Xray，不再提供模式切换）、`/api/panel/mode`（不再支持外部后端切换）。底层 Manager 方法仍保留给控制台内部使用。

后续可把 `console` 改为 SSE；ETag 已就绪：`GET /api/console` 按可见版本号回 `ETag`，`If-None-Match` 命中（或 `*`）回 304 空体，语义与版本轮询一致，旧客户端忽略即可。当前前端仍走版本轮询，切条件 GET 时再改前端。

## 外部后端迁移

- 旧 `panel_mode` 为 `3x-ui` 或 `xray-cf-lite` 时，服务启动会直接失败并给出提示；不会静默切到 native，避免外部面板仍占用端口。
- 迁移前备份外部面板数据，停止外部面板服务，删除或重命名工作目录下的 `panel_mode`，然后重新创建内置 Xray 入站并更新订阅。
- 本项目不导入 3x-ui 数据库或 xray-cf-lite 配置，也不会删除这些外部数据。

## 测试

```sh
cd vps/fanout/frontend
npm run typecheck
npm run test:unit
npm run build
```

Vitest 覆盖纯逻辑、API 客户端、主题、状态/订阅/自动化格式化、共享组件和登录页（含错误提示与空密码拦截）；页面集成测试用 Vitest、`@vue/test-utils`、happy-dom 和内存路由挂载真实控制台，mock `fetch` 覆盖根路径与导航、出口目标弹窗与地区库存、出口策略与换 IP、删除自动出口及归档清理选项、归档入口清理、SOCKS5 凭据编辑、入口复制、订阅格式/二维码、节点筛选、接入页签、主题菜单和移动端导航。旧的 `tests/test_console_*.mjs` 已随旧控制台删除。

## 已完成切换

1. 主控制台页面、登录页和设置页已迁移到 Vue。
2. `/` 已指向新控制台，`/next/` 保留为别名。
3. 旧 `console.html`、旧 `login.html` 和旧控制台 Node 测试已删除。
4. 入站 CRUD、客户端和导出已并入「接入 → 高级接入」；`/advanced` 只保留重定向。

## 当前边界

- `/advanced` 只做重定向；旧页面和样式已删除。
- 候选列表由 `/api/nodes` 服务端分页（`limit`/`offset`，默认 200/页）返回；不带 `limit` 时保持旧全量形状供外部调用。
- `console` 全量不再 5 秒轮询，改为小版本轮询 + 变化拉取 + 60 秒兜底；ETag、SSE 和凭据按需加载尚未实现。
- 页面集成测试使用 mock API 覆盖主流程，不替代真实 VPS 的隧道、防火墙、SSTP 和第三方代理验收。
- happy-dom 没有布局引擎，移动端横向溢出仍需在真实浏览器或设备上验收；移动端底部导航的结构已由集成测试覆盖。
- 需要在真实 VPS 上验收登录、随机路径、移动端、慢网、出口操作、SSTP 和第三方代理。
- 开启外部 SOCKS5 和保存第三方代理会重载 Xray。若通过反向代理访问，请让代理读超时覆盖重载时间（建议至少 120 秒），否则可能收到 502；服务端现在会把 5xx 和 panic 写入日志，前端会刷新状态而不是停在错误上。
