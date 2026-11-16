# 发布

vgatebox 以 `vX.Y.Z` 标签发布 Linux amd64 完整包。发布由 CI 从通过测试的提交构建；不接受手工拼接的包。

## 版本与标签

- 版本格式：`vMAJOR.MINOR.PATCH`，例如 `v1.5.7`。
- `VERSION` 文件、`BUILD.json.version` 和 GitHub tag 必须一致。
- 发布包要求源码干净（`source_dirty=false`）且由源码构建（`manager_origin=built_from_source`）。
- 不要移动或覆盖已发布的 tag；需要修复时发新 patch 版本。

## 构建

### 前端

```sh
cd vps/fanout/frontend
npm ci --ignore-scripts
npm run build
```

产物在 `vps/fanout/webdist`；Go 通过 `embed` 打进二进制。缺少 `webdist/index.html` 时打包脚本会失败。

### 完整包

```sh
python3 scripts/build_vps_package.py --version vX.Y.Z
```

可选参数：

| 参数 | 说明 |
| --- | --- |
| `--manager PATH` | 使用已有二进制（标记 `supplied_binary`，不能用于在线发布） |
| `--xray-archive PATH` | 本地 Xray 归档缓存；仍按 `config/xray-release.json` 的 SHA256 校验 |
| `--output PATH` | 输出目录，默认 `dist` |

输出：

```text
dist/
  vgatebox-linux-amd64.tar.gz
  checksums.txt
```

包内容：

```text
VERSION
BUILD.json
package-sha256.json
bin/vgatebox-manager
bin/xray
src/
config/settings.json
scripts/toolbox.py
scripts/vps_acceptance.py
deploy/
licenses/
UPSTREAM.md
install.sh
LICENSE
LICENSE.reference
```

### 工具链

| 工具 | 版本 |
| --- | --- |
| Go | 1.26.11 |
| Node | 22 |
| Python | 3.12（CI）；工具箱支持 3.9+ |
| Xray | `config/xray-release.json` 固定版本 |
| Mihomo（仅测试） | `tests/mihomo-release.json` 固定版本 |

## CI 发布流程

### 自动发布

推送 `vX.Y.Z` tag：

```sh
git tag vX.Y.Z
git push origin vX.Y.Z
```

`.github/workflows/vps.yml`：

1. 构建前端。
2. Linux Go 测试（sudo）、race 子集、卸载验证。
3. 从测试提交构建完整包。
4. Debian 12/13、Ubuntu 22.04/24.04 安装矩阵。
5. `release` job 校验包并创建 draft release。
6. 上传 `vgatebox-linux-amd64.tar.gz`、`checksums.txt`、`install.sh`、`uninstall.py`。
7. 发布为 latest。

发布前校验：

- `checksums.txt` 通过；
- 包内路径无绝对路径/`..`/重复成员；
- `package-sha256.json` 完整；
- `VERSION == BUILD.json.version == tag`；
- `BUILD.json.git_head == 当前提交`；
- `source_dirty=false`、`manager_origin=built_from_source`。

任何一项不匹配，发布 job 失败。

### 手动补发

`.github/workflows/release-vps.yml`：

- 输入一次成功的 `VPS tests and package` run ID 和 tag；
- 校验 run 结论、workflow 名称、提交；
- 下载该 run 的 artifact；
- 校验包；
- 创建/更新 draft release，上传并发布。

用于回填/重试发布，不重新构建。已发布 release 不会被覆盖。

### 共享候选池

`.github/workflows/update.yml` 仅手动运行，构建共享候选池 artifact；它不发布版本、不连接 VPN，也不替代用户 VPS 验收。

## 发布前检查

- [ ] `main` 上所有测试通过（`test.yml`、`lint.yml`、`e2e.yml`、`vps.yml`）。
- [ ] 前端构建产物可复现；`webdist/.gitkeep` 仍在。
- [ ] `config/xray-release.json` 的版本/SHA256 正确。
- [ ] `docs/` 已更新；没有历史叙事或断开链接。
- [ ] `README.md` 的安装命令、支持范围和边界与代码一致。
- [ ] 更新 `THIRD_PARTY.md`（如果依赖/上游变化）。
- [ ] Release Notes 写清行为变化、兼容性、升级注意事项。
- [ ] 备份/回滚方案可用。

## 发布后验证

1. 用 README 的统一安装命令在干净的 Debian 12/Ubuntu 24.04 上安装。
2. `vgatebox access` 登录。
3. 启用本机网络，导入订阅，访问网页。
4. 创建 1 个 VPN Gate 出口，等待就绪，更新订阅。
5. `vgatebox check-update` / `vgatebox update` 从上一个版本升级。
6. 检查备份、健康检查、卸载。
7. 记录验证结果；未覆盖的系统/客户端在 [边界与兼容](../reference/limits.md) 标注。

## 版本策略

- **patch**：修复、兼容性改进、文档修正。
- **minor**：新功能、行为改进，保持现有配置/订阅兼容。
- **major**：不兼容的配置/状态/订阅变化；需要迁移说明。

配置/状态 schema 有版本号；读不懂的新版本 fail-closed。发布前用迁移夹具验证旧状态可读。

## 安全与供应链

- 只从 GitHub Releases 正式版更新；draft/prerelease 不进入在线更新。
- Xray 二进制按固定 SHA256 校验；SSTP 客户端在 CentOS 从固定版本源码构建。
- 发布包不包含 token、运行状态、测试内核或本地缓存。
- `scripts/build_vps_package.py` 只打包受管文件；`source_identity()` 记录源码快照。
- 依赖许可和上游来源见 [THIRD_PARTY.md](../../THIRD_PARTY.md)。

## 相关

- [安装器](installer.md)
- [测试](testing.md)
- [更新](../operate/update.md)
- [安装](../operate/install.md)
- [边界与兼容](../reference/limits.md)
