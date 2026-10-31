# VPN Gate 参考项目审计

核查日期：2026 年 10 月 7 日。核查对象为下表提交的仓库文件、工作流和许可证；链接固定到提交，避免后续上游更新改变结论。本次没有部署这些项目，也没有把其实现源码复制进本项目。

设计说明与开发基准见 [THIRD_PARTY.md](../THIRD_PARTY.md)。三个部署 Profile 共用同一 Pool Builder 和数据契约，VPS Standalone 可独立生成本地池。

后续执行状态：本项目已基于本审计的固定提交和 SHA256 准备、修改与部署 GPL v3 Checker 和 GPL v2 EdgeTunnel，保留完整许可和来源，不合并两个程序；并引入 MIT fanout 作为运行底座，保留完整许可，节点来源使用共享 `node_pool.json`。正式复用清单见 [THIRD_PARTY.md](../THIRD_PARTY.md)。下文“本次没有部署”等表述描述原始源码审计时点，不再代表当前执行状态。

## 1 许可证与复用边界

| 项目 | 固定提交 | 许可证证据 | 本项目处理方式 |
| --- | --- | --- | --- |
| hezhanleiok/gate | `b397dd897dce70f188343bb8a20626ea19bcaedc` | [完整文件树](https://github.com/hezhanleiok/gate/tree/b397dd897dce70f188343bb8a20626ea19bcaedc) 未发现 LICENSE、COPYING 或源文件授权声明；GitHub API license 为 null | 仅参考抓取与产物设计；不得复制实现、工作流或页面模板 |
| lsh8848/cm-Workers-CheckSocks5 | `2f31cf9a242444eb22905d888d4d8871b0625b3a` | [LICENSE](https://github.com/lsh8848/cm-Workers-CheckSocks5/blob/2f31cf9a242444eb22905d888d4d8871b0625b3a/LICENSE) 为完整 GPL v3；[README](https://github.com/lsh8848/cm-Workers-CheckSocks5/blob/2f31cf9a242444eb22905d888d4d8871b0625b3a/README.md) 声明 GPL v3 | 可在履行 GPL v3 条件并完成文件来源核查后提取、修改 SSTP/PPP 等模块；不得改标为 MIT |
| xibabro/CF-vpngate | `28cc97e825180818c132604240fe0859d4f2b5e9` | [完整文件树](https://github.com/xibabro/CF-vpngate/tree/28cc97e825180818c132604240fe0859d4f2b5e9) 未发现许可证或源文件授权声明；GitHub API license 为 null | 仅参考最终链路测试方法；自行编写抓取器、测试器、订阅生成器和 Actions |
| byJoey/fanout | `d7ce5224caf3469b19876982abdf8d1be16c4a8b` | [LICENSE](https://github.com/byJoey/fanout/blob/d7ce5224caf3469b19876982abdf8d1be16c4a8b/LICENSE) 为 MIT，Copyright 2026 Joey | 优先复用为 Mode B 底座；保留版权与完整许可声明，记录来源、提交和修改；按固定 Slot 与断线禁止回退要求做必要的最小补丁 |
| Guli-Joy/aimili-vpngate | `56deecacf14dd3280dd01d96526d5891692c71c2` | [LICENSE](https://github.com/Guli-Joy/aimili-vpngate/blob/56deecacf14dd3280dd01d96526d5891692c71c2/LICENSE) 是缩略 GPL 文本，包含省略号及获取全文链接，授权段写明 GPL v3 或更高版本；GitHub API 为 Other / NOASSERTION | 有 GPL-3.0-or-later 授权意图，不应误称无许可证或禁止商用；按用户要求，本版仅作功能参考，暂不复制，待补齐许可证全文并核实上游及文件来源后再评估 |

“公开可读”不等于可以重新发布。无许可证仓库在取得明确授权前不纳入源码复用范围。依据：[GitHub 仓库许可说明](https://docs.github.com/en/repositories/managing-your-repositorys-settings-and-features/customizing-your-repository/licensing-a-repository)。

GPL 代码发布时需保留许可、版权、修改说明并提供适用的对应源码；与其构成一个派生程序的代码应按兼容条款发布。不能仅靠拆目录或接口名称宣称规避 GPL。建议新项目采用 GPL-3.0-only 作为许可方向，最终在开始引入代码时建立正式 LICENSE 与第三方清单。本次文档不替用户确定或发布项目许可证。具体义务参见上述 CheckSocks5 LICENSE 中第 4、5、6 节。

### 额外发现的 EdgeTunnel 依赖

三个参考项目涉及 `cmliu/edgetunnel`。额外核查提交 `af4f9837e1843e34159018713bc8749ccec3004d` 的 [LICENSE](https://github.com/cmliu/edgetunnel/blob/af4f9837e1843e34159018713bc8749ccec3004d/LICENSE)：完整 GPL v2，GitHub 标识 GPL-2.0。仅凭许可证附录中的示例“or later”不能证明所有项目源码选择了 GPL-2.0-or-later。因此不能直接把其整份 Worker 和 GPL v3 Checker 合成一个派生 Worker 后宣称许可已解决。

v0.1 优先通过最小适配器对接现有 Checker 与 EdgeTunnel，先跑通完整链路；共享协议模块或自建 Gateway 后续按需要考虑。若未来要复制或改发 EdgeTunnel，需要先明确版本选择及兼容性，分别履行该组件的许可义务。网络协议互通本身也不代表任意源码组合都获授权。

## 2 gate 的职责和问题

主要依据：[vpngate.py](https://github.com/hezhanleiok/gate/blob/b397dd897dce70f188343bb8a20626ea19bcaedc/vpngate.py)、[check.yml](https://github.com/hezhanleiok/gate/blob/b397dd897dce70f188343bb8a20626ea19bcaedc/.github/workflows/check.yml)。

职责是从官方 CSV 或 GitHub 镜像取节点，解码 OpenVPN 配置提取 TCP remote，转换为 SSTP 候选，调用检测 Worker，输出 `public/data.json`、页面及含 `$sstp://` 指令的 `nodes.txt`。最终 VLESS 参数由外部 EdgeTunnel 生成。

源码中的主要问题：

1. `classify_network` 把 `is_datacenter == false` 直接判为 residential，并在字段缺失时依靠 ISP 关键词或主机名猜住宅。证据不足。
2. `build_nodes_text` 把非 residential 的节点统一放进“机房”组，unknown 也可能被误标。
3. 把 OpenVPN TCP remote 当作 SSTP 候选入口，只能作为启发式发现，不能说明该端口确实支持 SSTP。
4. `check_one` 依据 Worker 的 `success` 接受节点，没有最终 VLESS/Xray 测试，也没有两阶段 IP 相等检查。
5. 默认检查并发 32、节点数上限 0，没有独立的画像 API 预算、指数退避和字段完整性准入。
6. 保留 CSV 国家作为主分组，缺少 advertised country 与实测 exit country 的明确区分。
7. README 宣称每 30 分钟更新，但此提交的 `check.yml` 仅有 `workflow_dispatch`，没有 schedule。
8. 能拦截“所有 Worker 请求异常”的情况，但没有完整的新鲜度协议和整批版本原子发布设计。

可以借鉴主源/镜像、去重、页面和节点清单的组织思路；不能复制未获授权的实现。

## 3 CheckSocks5 的 SSTP 检测机制

依据：[_worker.js](https://github.com/lsh8848/cm-Workers-CheckSocks5/blob/2f31cf9a242444eb22905d888d4d8871b0625b3a/_worker.js)，重点为 `checkProxy`、`createSstpClient`、`createTcpOverPppSession`、`sstpConnect` 和 `TlsClient`。

实际链路如下：

1. 用 `cloudflare:sockets.connect` 和 `secureTransport: on` 建立到 VPN Gate 的 TLS 连接；域名保留用于 SNI。
2. 发送 SSTP_DUPLEX_POST，协商 SSTP 控制连接和 PPP 封装。
3. 完成 PPP LCP；需要认证时支持 PAP，其他 PPP 认证协议在该路径中拒绝。
4. IPCP 先请求地址 `0.0.0.0`，处理 Configure-Nak 提供的地址，再请求并获取 IPv4。这个地址是 PPP 内部地址，不能当作公网出口。
5. 解析目标 IPv4，在 PPP 内构造 IPv4/TCP 报文、校验和及 SYN/ACK，建立实际 TCP 数据通道。
6. 将通道包装为 readable/writable，再用用户态 TLS 客户端访问 `www.iplocate.io:443` 的 `/api/lookup`，读取 HTTP 与 JSON。
7. 将返回信息映射成出口 IP、ASN、ISP 与隐私标签，关闭连接并输出结果。

它确实做了 SSTP、PPP、IPCP 和隧道内数据传输，远强于 TCP 443 探测；但仍有必须修复的边界：

- `privacy?.is_hosting || false` 等映射把“字段缺失”变成 false，丢失 unknown。新适配器必须保留三态和原始响应。
- 用户态 `TlsClient.acceptCertificate` 只检查证书非空，TLS 1.3 的 CertificateVerify 分支只记录消息。所检查实现未完成可信 CA、主机名及证书签名认证；校验 Finished 不能替代服务器身份验证。不能把这条查询结果直接当成可信画像证据。
- SSTP、PPP、用户态 TCP 是有限的兼容实现；认证方式、控制包处理、超时、背压和长连接需要协议测试，不能据一次小请求成功宣称支持任意流量。
- 没有内置检测 TOKEN 鉴权，不能直接作为公开、不受限的扫描接口部署。
- 第一阶段成功仍不能代替最终 VLESS 链路成功。

v0.1 保留真实协议握手机制，通过现有 Checker 获取标明来源的初步基线，最终使用正常校验证书的 curl 和两个独立 HTTPS 出口端点核验。Checker TLS 身份认证缺陷记录为技术债；双端点比对不能消除该缺陷。修复安排和证据强度见开发文档，暂不为 MVP 自建签名 Echo 服务。

## 4 CF-vpngate 的 Xray 验活机制

主要依据：[test_nodes.py](https://github.com/xibabro/CF-vpngate/blob/28cc97e825180818c132604240fe0859d4f2b5e9/scripts/test_nodes.py)、[vpngate-daily.py](https://github.com/xibabro/CF-vpngate/blob/28cc97e825180818c132604240fe0859d4f2b5e9/vpngate-daily.py)、[sync.yml](https://github.com/xibabro/CF-vpngate/blob/28cc97e825180818c132604240fe0859d4f2b5e9/.github/workflows/sync.yml)。

每个 VLESS 链接解析 UUID、入口、TLS、WebSocket 参数，生成临时 SOCKS 入站与 VLESS 出站，启动 Xray，等待本地端口，通过 `curl --socks5-hostname` 请求测试网站。配置构建器默认往 path 追加 `global=1`。成功条件是 curl 成功且 HTTP 状态以 2 或 3 开头；画像查询另经同一 Xray SOCKS 请求 ippure，失败或缺字段仍可保留有效节点。源码没有第一阶段 expected IP 与最终 actual IP 的严格等值准入。

还需注意：

- 检测时修改后的 path 与最终订阅生成的原 path 不一致；`fdip_path` 生成函数本身不含 `global=1`。因此“测试用了强制出口”不等于“用户拿到的配置也用了强制出口”。
- `build_xhttp.py` 会派生 XHTTP 订阅，但所检查的 `build_xray_config` 只构建 wsSettings，没有对应 XHTTP 测试配置。不能把 WS 验活结果直接贴到 XHTTP 产物。
- `sstp_check.py` 是另一个低层握手工具，默认仅验证 IPCP，可选做 TCP 三次握手；该脚本不等同于最终 Xray 验活，sync 工作流也未把它作为两阶段出口基线。
- 主 sync 工作流仅 `workflow_dispatch`，注释依赖外部服务器触发；不符合本项目 Mode A 完全无需 VPS 的部署目标。
- 有“入口 TCP 预检、有效比例过低就保留旧库”的保护思路，但退出码 0 和旧数据没有有效期会掩盖过期结果。
- 抓取器按 hostname 增量追加，已经存在的 hostname 不更新 IP/端口，可能留下过时入口。
- 下载 Xray latest 且仅验证 ZIP 格式，没有固定版本和发布文件摘要校验。

新项目独立实现测试编排，并把最终配置摘要、出口相等和证据有效期纳入准入。

## 5 fanout 的八项设计结论

依据：[tunnel.go](https://github.com/byJoey/fanout/blob/d7ce5224caf3469b19876982abdf8d1be16c4a8b/tunnel.go)、[netns.go](https://github.com/byJoey/fanout/blob/d7ce5224caf3469b19876982abdf8d1be16c4a8b/netns.go)、[netnsguard.go](https://github.com/byJoey/fanout/blob/d7ce5224caf3469b19876982abdf8d1be16c4a8b/netnsguard.go)、[health.go](https://github.com/byJoey/fanout/blob/d7ce5224caf3469b19876982abdf8d1be16c4a8b/health.go)、[manager.go](https://github.com/byJoey/fanout/blob/d7ce5224caf3469b19876982abdf8d1be16c4a8b/manager.go)、[xui.go](https://github.com/byJoey/fanout/blob/d7ce5224caf3469b19876982abdf8d1be16c4a8b/xui.go)、[state.go](https://github.com/byJoey/fanout/blob/d7ce5224caf3469b19876982abdf8d1be16c4a8b/state.go)。

| 研究项 | 源码实际行为 | 新项目决定 |
| --- | --- | --- |
| 独立 namespace | 每个 Tunnel 有 Slot；创建 netns、veth、/30 网段，在 netns 中运行 OpenVPN，tun 名均可为 tun0 | 每个 Slot 的运行代独立 netns；tun 命名可按槽位表达，不能依靠 tun 名代替隔离 |
| 路由隔离 | `ip netns exec ... openvpn` 让推送路由落在隧道 namespace；母机另设转发和 NAT | 保留隔离，同时限定母机规则作用范围，绝不修改母机默认路由 |
| SOCKS 转换 | 母机监听，拨号时 LockOSThread + setns，在目标 namespace 创建 socket 后恢复原 namespace | v0.2 优先以 fanout 为运行底座，验证恢复与母机命令保护；以最小补丁满足目标 tun 绑定与禁止回退，不先引入独立 agent |
| 固定槽位 | 换节点重用 Tunnel 对象的 Slot、Port；状态落盘供恢复 | Slot 身份单独持久化，端口和 tag 都脱离节点 |
| 掉线换后台 | reconnect 杀旧 OpenVPN、拆重建 netns、重试候选，原 listener 通常保留 | listener 始终稳定；切换期间拒绝业务；旧连接关闭；禁止自动改端口 |
| 假在线检测 | netns 经 veth/NAT 可从母机出网；健康检查不只看连通性，而是比较当前 IP 与 t.ExitIP | 内核先阻断泄漏，再通过固定 SOCKS 和 Xray 测出口；不能等健康检查再补救 |
| expected 与 actual | t.ExitIP 是连接初期 `probeExitIP` 得到的值，后续通过 namespace 内 HTTP ipify 比对 | 本机按目标协议独立取得 tun 基线，再从固定 SOCKS 取 actual；不要求 runner B Full Chain，不在健康检查中静默修改 expected |
| 3x-ui / Xray | 读取面板 Xray 模板、更新 SOCKS outbounds/routing、重载 Xray；tag 是 `fanout-<hostname>`，换节点后 Rebind | v0.4 一键导出可粘贴标准 JSON；使用持久化 Slot tag，换节点无需改外部路由；API 接管后续可选 |

### 不能照搬的具体行为

- `setupNetns` 给 namespace 配经 veth 的默认路由，并对整个槽位子网做 MASQUERADE 与双向 FORWARD ACCEPT；所检查路径没有针对普通代理流量的 kill switch。检测发现泄漏不代表泄漏从未发生。
- 健康检查间隔 10 秒、连续 2 次失败触发重连；调用串行进行，不能据此承诺固定 20 秒恢复。
- `serve` 监听 `0.0.0.0`；端口反复绑定失败后会改成另一个随机端口。新要求应绑定 loopback，端口冲突报错并保留配置。
- listener 在 exit IP 探测完成前可能已启动；Socks 拨号路径没有检查“已验证健康”状态，需增加业务门禁。
- `candidatesFor` 主要限制同地区；地区缺失时允许任意地区，没有完整 ISP、出口类型、实测延迟策略。
- `vpngate.go` 的住宅判断主要排除 public-vpn 和一个自营网段，其余甚至非法 IP 默认通过；不能作为新节点池的分类依据。
- `netnsguard.go` 专门处理 Go 线程切 namespace 后子进程可能继承错误 namespace 的问题。新实现必须明确 host 与 namespace 的进程边界，不能只写一个 setns 包装器就认为安全。

## 6 AimiliVPN 可参考的功能

依据：[README](https://github.com/Guli-Joy/aimili-vpngate/blob/56deecacf14dd3280dd01d96526d5891692c71c2/README.md)、[vpngate_manager.py](https://github.com/Guli-Joy/aimili-vpngate/blob/56deecacf14dd3280dd01d96526d5891692c71c2/vpngate_manager.py)、[proxy_server.py](https://github.com/Guli-Joy/aimili-vpngate/blob/56deecacf14dd3280dd01d96526d5891692c71c2/proxy_server.py)。

可参考槽位独立国家/ISP策略、手动指定节点、换 IP、停止后保留槽位、删除不重排其他端口、Xray JSON 导出和真实出口检查等产品功能。技术路线是 `SO_BINDTODEVICE + 独立策略路由表`，并非 fanout 的每隧道 namespace，不能把两种实现混为一谈。

所检查的 `check_slot_egress` 主要接受 curl 成功及非空短文本，循环将其写为 exit_ip；没有按 validated pool 的 expected IP 作严格等值比较。导出 tag 还含国家，因此直接照搬会与“修改国家仍保留 tag”的新要求冲突。新项目独立实现这些功能，不复制该项目代码或页面。

## 7 重复能力与整合位置

| 重复能力 | 现有实现分布 | 新项目唯一归属 |
| --- | --- | --- |
| VPN Gate CSV 抓取、配置解码、去重 | gate、CF-vpngate、fanout、AimiliVPN | 同一 Pool Builder：src/vpngate.py 与 pool.py，可在 Actions 或 VPS 本机运行 |
| SSTP / PPP 与候选过滤 | Checker、CF-vpngate 低层 probe、EdgeTunnel | 现有 Worker 与最小适配器；共享协议包后续按需提取 |
| IP 画像与“住宅”判断 | gate、Checker、CF-vpngate、fanout、AimiliVPN | 共享 classifier.py / intelligence adapter；区分 strict/likely，按实际协议出口保存证据；VPS 缺少画像 API 时保持 unknown |
| 最终链路测试 | CF-vpngate、AimiliVPN 部分出口自检 | A 验证实际 VLESS；B 由目标 VPS 验证本机 Slot，复用比较规则 |
| 节点选择、重连与多出口 | fanout、AimiliVPN | fanout 底座上的 Multi Exit Manager，读取本地或远程同结构 node_pool.json；Standalone 调用共享 Pool Builder，不保留第二套抓取/分类实现 |
| 订阅、页面与 Xray 配置 | 多仓库各自生成 | consumers/exporters 与 WebUI |

## 8 官方约束来源

- [Cloudflare Workers limits](https://developers.cloudflare.com/workers/platform/limits/)：核查时 Free 为 100,000 请求/天、HTTP CPU 10 ms、128 MB、50 子请求/次；每 invocation 建立阶段同时连接限制 6。不能把它误解为账户只能 6 条连接，或照搬旧文档的 Paid 子请求数字。
- [Cloudflare TCP sockets](https://developers.cloudflare.com/workers/runtime-apis/tcp-sockets/)：外层 connect 对 Cloudflare 地址及私网等存在限制；不提供任意 UDP socket。
- [GitHub Actions limits](https://docs.github.com/en/actions/reference/limits)：托管 job 最长 6 小时；本项目应主动采用更短预算。
- [GitHub schedule](https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows#schedule)：调度可能延迟或丢弃；公开仓库长期无活动可自动禁用 schedule。
- [IPLocate 配额](https://www.iplocate.io/docs/getting-started/quota-rate-limits)：无 key 每源 IP 50 请求/天，免费 key 1,000/天；429 是画像服务故障或限额，不应误判 VPN 本身掉线。
- [IPLocate 出口查询](https://www.iplocate.io/docs/ip-intelligence-api/get-current-ip-free-api)：`api.iplocate.io` 提供仅 IP 的端点，与完整画像 API 分离。原 Checker 使用的 `/api/lookup` 不能假设长期无需 key、不限量。
- [Linux network namespaces](https://man7.org/linux/man-pages/man7/network_namespaces.7.html)：网络设备、路由、防火墙和端口等隔离。
- [OpenVPN 2.6 manual](https://openvpn.net/community-docs/community-articles/openvpn-2-6-manual.html)：路由、降权、配置与脚本行为的实现依据。
- [Xray SOCKS outbound](https://xtls.github.io/en/config/outbounds/socks.html) 与 [routing](https://xtls.github.io/en/config/routing.html)：标准导出格式及规则匹配依据。

以上为源码审计与官方资料核对，不等于参考项目已在当前 Cloudflare、GitHub 或真实 Linux VPS 上通过集成测试。运行验证安排见 [开发与测试](TESTING.md)；既有源码事实保留，项目设计按分阶段方案执行。
