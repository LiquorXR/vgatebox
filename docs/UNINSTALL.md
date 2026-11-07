# 卸载与旧版清理

支持正式卸载命令。在 VPS 以 root 执行；普通命令默认保留 `/var/backups/vgatebox` 中已有备份，程序、配置、管理密码、订阅凭据及检测记录会删除。执行前列出路径，并要求输入 `yes` 确认。

先预览彻底清理范围：

```sh
vgatebox uninstall --dry-run --purge --legacy
```

确认后卸载当前项目、已有备份及可确认归属的默认旧 fanout：

```sh
vgatebox uninstall --purge --legacy
```

只卸载当前项目并保留备份，用 `vgatebox uninstall`。`--purge` 同时永久删除本项目备份；`--legacy` 额外清理标准旧版的 `/var/lib/fanout`、`fanout.service`、`/usr/local/bin/fanout`，以及具有旧菜单签名的 `/usr/local/bin/f`。无关的 `f` 工具保留。自动化需明确添加 `--yes`，不接受隐式管道确认。

## 卸载中断后

如果 `vgatebox uninstall` 不可用，可以直接下载固定版本、独立运行的卸载工具，无需先启动代理：

```sh
curl -fsSL https://github.com/LiquorXR/vgatebox/releases/latest/download/uninstall.py -o /tmp/vgatebox-uninstall.py
python3 /tmp/vgatebox-uninstall.py --dry-run --purge --legacy
python3 /tmp/vgatebox-uninstall.py --purge --legacy
```

临时下载文件可以在完成后自行删除。

## 清理和保留的边界

清理标准 systemd 安装的三个单元、程序与两个命令、环境配置、项目数据和部署状态。网络清理同时核对实例名称、持久网段、出口/检测资源记录和实际地址；按具体规则删除 NAT 和转发项、namespace、veth 及其专用 DNS 目录，不刷新全局防火墙。宿主机 `/run/netns` 下由崩溃或私有 mount namespace 遗留的失效普通文件，只有在该槽位有本项目记录时才会清理；无法确认归属的同名条目保留现场。

外部面板（如 3x-ui / xray-cf-lite）的数据、其他实例、其他 namespace/接口/规则、系统共享依赖（包括 CentOS 上本机构建的 `vgatebox-sstp-client` RPM）、全局 IP 转发设置及系统日志保留；外部面板已不再是本项目支持的入口后端，但本项目不会删除其数据。部署协调锁由系统管理，保留到系统清理，避免卸载与安装间的竞争。旧版安装没有记录修改前的全局 IP 转发值，因此不猜测原值。许可证和上游来源说明继续保留在源码中；过时的独立安装脚本、f 菜单及服务示例已从当前仓库移除。

安装/更新共用部署锁。未完成部署、活动更新、实例仍运行、服务停止失败、符号链接/挂载点、网段冲突或缺少归属证据时中止，不能报告卸载成功。中止可能已停止部分本项目服务；核对原因后可用独立工具重试。文件删除阶段失败时也如实报未完成，不承诺自动回滚删除的数据。

自动清理只支持标准目录和标准 systemd 单元。自定义工作目录、服务参数/覆盖配置及旧 OpenRC/SysV 服务须先核对归属；工具会保留现场，不能按名称批量强删。任意手工下载目录、用户另存的归档和共享系统日志不属于本项目受管路径。

隔离 Linux 测试不等于已在用户 VPS 执行卸载。unknown 不称家宽。
