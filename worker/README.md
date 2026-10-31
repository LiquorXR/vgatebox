# 第一轮 Worker 适配与部署准备

第一轮曾通过 HTTP/VLESS 对接两个独立 Worker。现在统一入口 [`vgatebox/`](vgatebox/) 已改为自包含部署：管理、订阅、节点检查和 TCP-only 数据面都在同一个 Worker 中运行，只绑定一个 KV Namespace，不依赖旧 Worker。

项目自己的管理、订阅、检查与统一入口路由在 [`control/`](control/)，公开发布入口在 [`vgatebox/`](vgatebox/)。它读取统一验证池的 KV manifest，提供管理页、受保护订阅、SSTP 出口检查和数据面 WebSocket 入口。

执行 `python3 scripts/prepare_workers.py` 从固定提交下载并校验源文件和完整许可证；或通过 `--source-cache` 读取已审计文件。生成目录是忽略的 `runtime/deploy/checker`、`runtime/deploy/edge`，各自包括 `worker.mjs`、`upstream.mjs`、`LICENSE`、`SOURCE.json` 和 `wrangler.toml`。两份程序分别修改、分别部署、分别履行 GPL v3/GPL v2 义务。不要拼接两份部署包或删去版权和许可证。源程序的功能描述注释不是许可依据；许可依据和固定来源见 [THIRD_PARTY.md](../THIRD_PARTY.md)。

准备部署名：`vpngate-checker`、`vpngate-edge`。生成的 `pages-upload.zip` 可通过 Pages 高级模式直接上传，执行 `_worker.js`，域名为确认创建后的 `*.pages.dev`。Checker Secret 为 `CHECKER_TOKEN`，至少 32 个字符；Edge Secret 为 `UUID`，必须为随机 UUID v4。不得硬编码到源文件或提交 Git。未设置有效 Secret 时，业务入口返回 503；`GET /health` 仅显示服务名、版本和配置是否完整，不输出凭证。`.env` 保存确认后的域名与本机凭证，权限应为 600。

新增 Checker 入口只接受带 Bearer Token 的 `GET /check?proxy=sstp://...`，只允许官方 `*.opengw.net` 节点。拒绝其他协议、路径、重复参数和任意 IP；关闭原网页 UI，因此不会加载其网页统计脚本。当前范围不是任意外部代理检测服务。

实际部署后，原隧道内 `www.iplocate.io/api/lookup` 返回 429。因此生成版本改为官方无需 API Key 的 `api.iplocate.io/json`，仅取得隧道 IP 基线，保留 T1 身份认证技术债。删除原转换中的缺失隐私字段 false 默认值，不制造 ASN/ISP。完整画像由已有独立 HTTPS intelligence adapter 取得；未配置或证据不足时保持 `unknown`，不能凭官方 CSV 国家、主机名或连通性标成住宅。

新增 Edge 入口只接受 `/` 的 WebSocket、唯一 `sstp` 参数和 `globalproxy=1`，拒绝 `proxyip`、路径代理、其他参数与非 WebSocket 管理入口。仅允许官方 VPN Gate 域名，以及失败对照专用 `nonexistent.invalid`；UUID 仍由上游协议解析器检查。运行绑定只保留 UUID；生成版本拒绝 VLESS UDP，并关闭所有 UDP 转发 helper。仍须经过真实失效后端对照，不能只凭入口检查宣称隧道没有回退。

本地验证：`node tests/test_worker_guards.mjs`，以及对两份生成的 `worker.mjs` 执行 `node --check`。边界检查通过不等于 SSTP/PPP 或 Cloudflare 运行环境的集成测试。部署与许可边界见 [参考审计](../docs/REFERENCE_AUDIT.md)。

Checker：`CHECKER_URL` 指向 `/check`，工具发送 `GET ?proxy=sstp://username:password@hostname:port`。只接受 `type=sstp`、`success=true`、入口匹配以及合法公网 IPv4 的响应。正常响应的 `exit.privacy` 等原始数据保留；旧派生 `is_datacenter=false` 不当作住宅证据。

EdgeTunnel：适配器 `edgetunnel-globalproxy-v1` 生成 `/?sstp=<URL编码的账号@host:port>&globalproxy=1`，外层 VLESS 链接中的 path 再进行一次 URL 编码。这个接口针对审计的 `af4f9837e1843e34159018713bc8749ccec3004d`，不是所有同名 Worker 的通用约定。配置 `edge_base_path` 可指定已有基础路径。测试使用发布链接重新解析出的配置；不得在测试里临时追加其他参数。

工具必须先执行不存在 SSTP 后端的反向测试，随后执行真实节点两轮 HTTPS 出口核验。上游 UDP 能力不在本版验证范围内，生成的 Xray 配置拒绝 UDP 与字面 IPv6 目的地址；使用订阅的其他客户端也须按 TCP/IPv4 配置，不能据此宣称任意客户端 UDP 已安全。

原 Checker 的内层用户态 TLS 身份认证问题 T1 尚未修复，报告保留 `preliminary_checker_tls_T1`。最终 curl 保持正常 HTTPS 证书验证；可选画像接口使用独立正常 HTTPS 查询。双 Echo 不是 T1 的安全修复。

Checker 的 TOKEN/Bearer 或 Cloudflare Access 头只供已配置相应鉴权的部署使用，原项目本身不内置 TOKEN 鉴权。部署与授权要求见 [参考审计](../docs/REFERENCE_AUDIT.md)。
