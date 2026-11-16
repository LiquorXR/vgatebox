# 备份与恢复

vgatebox 在安装和更新前自动备份本项目受管文件。备份用于更新失败回滚、重装系统前迁移和误操作恢复；它不是整机镜像。

## 备份位置与内容

```text
/var/backups/vgatebox/
  install-<job-id>/project.tar.gz     # 安装/更新前备份
  update-<id>/install.log             # 网页更新的安装日志
```

备份归档包含本项目受管路径：

```text
var/lib/vgatebox/                  # 候选池、出口状态、检测、缓存、运行态
etc/vgatebox/                      # pool.json、intelligence.env
usr/local/lib/vgatebox/            # 程序、部署脚本、Python 工具箱
usr/local/bin/vgatebox
usr/local/bin/vgatebox-fanout
etc/default/vgatebox-fanout
etc/systemd/system/vgatebox-*.service
etc/systemd/system/vgatebox-pool.timer
```

不包含：

- 整机系统文件和其他程序。
- 服务商防火墙、DNS、反向代理配置。
- `/var/lib/vgatebox-deploy` 的部署事务状态（由安装器单独管理）。
- 运行中的临时任务目录。

备份目录权限 `0700`，归档 `0600`。归档仍含管理密码、订阅 token、第三方代理凭据、OpenVPN 配置和 SQLite 数据；不要上传到不可信存储。

## 自动备份

- 每次安装或更新前，安装器停止服务后创建 `install-<job-id>/project.tar.gz`。
- 更新失败时，安装器用这个归档恢复文件和服务状态。
- 备份不会自动清理；长期运行会累积，按需保留最近几份并删除旧归档。
- `vgatebox uninstall` 默认保留备份；`--purge` 才会删除 `/var/backups/vgatebox`。

## 手动备份

在 VPS 上执行：

```sh
systemctl stop vgatebox-fanout.service vgatebox-pool.timer
cd /
tar -czf /root/vgatebox-backup-$(date +%F).tar.gz \
  var/lib/vgatebox etc/vgatebox usr/local/lib/vgatebox \
  usr/local/bin/vgatebox usr/local/bin/vgatebox-fanout \
  etc/default/vgatebox-fanout \
  etc/systemd/system/vgatebox-fanout.service \
  etc/systemd/system/vgatebox-pool.service \
  etc/systemd/system/vgatebox-pool.timer
systemctl start vgatebox-fanout.service vgatebox-pool.timer
chmod 600 /root/vgatebox-backup-*.tar.gz
```

停止服务是为了让 SQLite 的 `-wal`/`-shm` 和状态文件保持一致。如果必须在线备份，至少先做 SQLite checkpoint，并接受状态可能不一致的风险。

备份后复制到另一台机器或加密存储；不要只留在同一块磁盘。

## 恢复

### 自动回滚

更新失败时优先让安装器自动恢复：

1. 重新运行安装入口或 `vgatebox update`。
2. 安装器发现未完成事务后会先执行回滚。
3. 用 `vgatebox update-status` 和 `systemctl status vgatebox-fanout` 确认。

不要手工删除 `/var/lib/vgatebox-deploy/transaction.json` 或 `recover.py`，否则会失去自动恢复能力。

### 手动恢复

只在自动恢复不可用、或迁移到新机器时使用。步骤：

1. 安装同版本或更新版本的 vgatebox（先让 systemd 单元和依赖就绪）。
2. 停止服务：

   ```sh
   systemctl stop vgatebox-fanout.service vgatebox-pool.timer
   ```

3. 解包备份到根目录（归档内路径是相对于 `/` 的）：

   ```sh
   tar -xzf /path/project.tar.gz -C /
   ```

4. 修正权限：

   ```sh
   chmod 700 /etc/vgatebox /var/lib/vgatebox /var/backups/vgatebox
   chmod 600 /etc/vgatebox/* /etc/default/vgatebox-fanout
   chmod 700 /var/lib/vgatebox/fanout
   ```

5. 启动并验证：

   ```sh
   systemctl daemon-reload
   systemctl start vgatebox-fanout.service
   systemctl start vgatebox-pool.timer
   vgatebox access
   curl -fsS http://127.0.0.1:8899/healthz
   ```

6. 登录管理页，检查出口、订阅和接入是否恢复；必要时重新检测候选或重建出口。

手动恢复会把备份中的管理密码、访问路径和订阅 token 一起带回来；旧客户端可以继续使用原地址。若只想恢复部分数据，优先恢复 `state/` 和 `etc/`，不要覆盖新版本程序文件。

## 迁移到新 VPS

1. 在旧 VPS 上停止服务并做手动备份。
2. 把备份复制到新 VPS（加密传输）。
3. 在新 VPS 安装 vgatebox。
4. 停止新服务，按“手动恢复”解包并修正权限。
5. 启动后检查管理地址、订阅、出口和防火墙。
6. 旧 VPS 保持停机一段时间，确认新实例稳定后再卸载。

迁移不会自动迁移服务商防火墙、DNS 记录和反向代理配置；这些需要单独处理。

## 恢复后验证

- `vgatebox access` 能显示地址和密码。
- `/healthz` 返回 `status: ok`，版本正确。
- 管理页能登录，出口状态与备份时一致。
- 订阅能拉取，客户端能连接。
- `systemctl status vgatebox-fanout` 无持续重启。
- 需要时重新检测候选；旧检测结果可能已过期。

## 相关

- [更新](update.md)
- [卸载](uninstall.md)
- [状态与文件](../reference/state-and-files.md)
- [故障排查](troubleshooting.md)
