package main

import "fmt"

// persistedSchemaVersion 是 state.json / native.json / console.json 三处
// 意图文件的 schema 版本。Python 侧直接读这些 JSON（installer、
// distro_verify、vps_acceptance），所以版本号必须同时是人可读的 JSON 字段，
// 不能藏进 SQLite。
//
// 兼容口径：
//   - 缺字段（0）= legacy，直接放行，走现有 normalize/默认值迁移；
//   - 1 = 当前，直接放行；
//   - >1 = 未来版本，fail-closed（拒绝启动 + migration_check 报 invalid）。
// 意图类状态永不 fail-open（见 ADR-0007）：读不懂的新版本宁可拒绝启动，
// 也不能把 Slot 意图猜错（错绑出口 = 流量走直连）。
const persistedSchemaVersion = 1

func validatePersistedSchemaVersion(v int, file string) error {
	if v < 0 || v > persistedSchemaVersion {
		return fmt.Errorf("%s 的 schema_version %d 不受支持（最大 %d）", file, v, persistedSchemaVersion)
	}
	return nil
}
