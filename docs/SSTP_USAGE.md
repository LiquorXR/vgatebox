# 原始 SSTP 的使用

这里的协议名称是 **SSTP**。本项目“原始 SSTP”导出的是 VPN Gate 连接参数及 VPS 观测，不是 FTP 文件服务，也不是自动生成的 VLESS 订阅。

## 本项目导出的内容

文本一行一个连接参数，示意：

```text
sstp://vpn:vpn@vpn-example.opengw.net:443
```

这是参数表示法，并不保证每个客户端都支持导入这个 URI。JSON 的 `kind` 是 `vpngate_vps_sstp_observations`，包含服务器域名、检测所用 IP、通过的端口、用户名/口令、观测时间、有效期和结果。只导出通过且仍有效的记录；VPS 测得可用不等于用户本地当前也可用。

## 直接连接的方法

在支持 SSTP 的客户端中使用复制的参数。Windows 可新建系统 VPN 连接，使用内置提供程序，VPN 类型选择 SSTP，服务器填写导出的 `域名:端口`，用户名和密码均为 `vpn`。如果端口是 443，可省略后缀；其他端口保留。服务器须使用 `*.opengw.net` 等官方域名正常验证证书，不能把导出中的检测用 IP 直接替换为服务器名。

连接关系是 `使用端 → VPN Gate SSTP`。保存参数后，数据不经过本 VPS，VPS 离线也不会因此中断这个独立连接；但 VPN Gate 节点仍可能下线或地址变化。使用端的 DNS、网络限制及协议支持也会影响连接。

依据：[VPN Gate 官方 SSTP 指南](https://www.vpngate.net/cn/howto_sstp.aspx)。本文未把官方操作指南当作本轮 Windows GUI 拨号实测。

v2rayN 的本项目订阅使用 VLESS；不能把上述 `sstp://...` 文本当成现有 VLESS 订阅。要在目前的 v2rayN 链路中使用 VPN Gate，可继续使用本项目 VPS 已转换的入口。
