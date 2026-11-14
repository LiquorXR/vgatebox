package main

import (
	"encoding/json"
	"fanout/internal/store"
	"fanout/internal/storesqlite"
	"log"
	"os"
)

// migrateLegacyIPTypeStore moves the pre-rename `ippure` MapStore to `ip_type`.
// Entries already present in the new store win; the old store is cleared only
// after the merged snapshot is durable.
func migrateLegacyIPTypeStore(db *storesqlite.DB) {
	if db == nil {
		return
	}
	legacy := db.MapStore("ippure")
	entries, err := legacy.LoadAll()
	if err != nil {
		log.Printf("旧 IP 类型缓存读取失败，保留原记录集: %v", err)
		return
	}
	if len(entries) == 0 {
		return
	}
	current := db.MapStore("ip_type")
	merged, err := current.LoadAll()
	if err != nil {
		log.Printf("IP 类型缓存读取失败，暂不迁移旧记录集: %v", err)
		return
	}
	if merged == nil {
		merged = map[string]store.Entry{}
	}
	for key, entry := range entries {
		if _, exists := merged[key]; !exists {
			merged[key] = entry
		}
	}
	if err := current.SaveAll(merged); err != nil {
		log.Printf("IP 类型缓存迁移写入失败: %v", err)
		return
	}
	if err := legacy.SaveAll(map[string]store.Entry{}); err != nil {
		log.Printf("旧 IP 类型缓存清理失败: %v", err)
	}
}

// migrateLegacyIPTypeFile renames the pre-rename JSON cache once. It is only
// used when SQLite is unavailable; an existing new file always wins.
func migrateLegacyIPTypeFile(legacyPath, path string) {
	if legacyPath == "" || path == "" || legacyPath == path {
		return
	}
	if _, err := os.Stat(path); err == nil {
		return
	}
	blob, err := os.ReadFile(legacyPath)
	if err != nil {
		return
	}
	var file ipTypeCacheFile
	if json.Unmarshal(blob, &file) != nil || file.Entries == nil {
		return
	}
	if err := atomicConfigWrite(path, blob); err != nil {
		return
	}
	_ = os.Remove(legacyPath)
}
