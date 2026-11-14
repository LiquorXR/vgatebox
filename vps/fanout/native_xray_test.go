package main

import (
	"encoding/json"
	"fanout/internal/paths"
	"os"
)

// writeXrayConfig 测试辅助：把配置写到磁盘，返回配置路径。
// 生产路径使用 Native.stageConfig；此 helper 仅供测试构造旧配置做迁移校验。
func writeXrayConfig(dir string, cfg map[string]any) (string, error) {
	blob, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", err
	}
	path := paths.Xray(dir)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, blob, 0600); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		return "", err
	}
	return path, nil
}
