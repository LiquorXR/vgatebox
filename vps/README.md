# vps

本目录是 vgatebox 的 VPS 运行时：Go 管理进程、内置 Xray、Vue 控制台、安装/卸载器和 systemd 单元。

- 安装、更新、卸载、安全与容量：[运维文档](../docs/operate/install.md)
- 产品概念和日常操作：[文档首页](../docs/README.md)
- 开发、测试、发布：[开发文档](../docs/develop/architecture.md)
- 上游来源与许可：[UPSTREAM.md](fanout/UPSTREAM.md) · [THIRD_PARTY.md](../THIRD_PARTY.md)

## 构建

```sh
cd fanout/frontend
npm ci --ignore-scripts
npm run build
cd ..
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -o ../../dist/vgatebox-manager .
```

完整包构建和发布流程见 [发布](../docs/develop/release.md)。

## 本地运行

```sh
sudo /usr/local/bin/vgatebox-fanout -dir /var/lib/vgatebox/fanout -panel native
```

生产安装、端口、防火墙和反向代理不要按本页手动拼；使用 [安装文档](../docs/operate/install.md)。
