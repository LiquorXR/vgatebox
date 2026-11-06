// Package store 把高频可再生的持久化从散落 JSON 文件收成 Store 接口。
//
// 范围（P3-3，ADR-0007）：只收调用方自有、不被 Python 读写的三类——
// 检测结果（checks.json）、出口观察快照（slot_health.json）、
// IPPure 画像缓存（ippure-cache.json）。
// 意图类文件（state/native/settings/console/automation/pool 共享态）
// 因 Python 侧（installer、distro_verify、vps_acceptance、pool_schedule）
// 直接读 JSON 而留在文件，SQLite 化等 Python 读者迁移后再议。
//
// 两类形状：
//   - MapStore：key → {data, checked_at, expires_at}，整存整取，对应
//     flushChecks 的合并写语义；TTL 过滤留在调用方，store 只做 dumb 存取。
//   - DocStore：单个 JSON 文档，对应 slot_health 这类快照。
//
// 后端：FileMapStore/FileDocStore（JSON 文件，0600，与现状字节兼容）；
// SQLite 后端见 internal/storesqlite，实现同一接口，切换只改构造处。
package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Entry 是一条带时间的 JSON 记录。时间零值表示“无 TTL 语义”，
// 由调用方解释；store 层只做存取，不做过期判断。
type Entry struct {
	Data      json.RawMessage
	CheckedAt time.Time
	ExpiresAt time.Time
}

// MapStore 以 key 寻址的记录集。SaveAll 是整表替换（事务语义由后端保证）。
type MapStore interface {
	LoadAll() (map[string]Entry, error)
	SaveAll(map[string]Entry) error
}

// DocStore 是单个 JSON 文档。
type DocStore interface {
	Load() (json.RawMessage, error)
	Save(json.RawMessage) error
}

func readJSONFile(path string) (json.RawMessage, error) {
	blob, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var v any
	if err := json.Unmarshal(blob, &v); err != nil {
		return nil, fmt.Errorf("invalid JSON document: %w", err)
	}
	out := append([]byte(nil), blob...)
	return out, nil
}

func writeJSONFile(path string, blob []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".store-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err := f.Chmod(0600); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(blob); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

// FileDocStore 把单个 JSON 文档存在 path。
type FileDocStore struct{ Path string }

func (s FileDocStore) Load() (json.RawMessage, error) { return readJSONFile(s.Path) }
func (s FileDocStore) Save(blob json.RawMessage) error {
	if !json.Valid(blob) {
		return fmt.Errorf("refusing to persist invalid JSON")
	}
	return writeJSONFile(s.Path, blob)
}

// FileMapStore 把 map[string]Entry 存成一个 JSON 对象文件，
// 形状与现状 checks.json / ippure-cache.json 的“对象套对象”一致。
type FileMapStore struct{ Path string }

func (s FileMapStore) LoadAll() (map[string]Entry, error) {
	out := map[string]Entry{}
	blob, err := os.ReadFile(s.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return nil, err
	}
	raw := map[string]json.RawMessage{}
	if err := json.Unmarshal(blob, &raw); err != nil {
		return nil, fmt.Errorf("invalid JSON map document: %w", err)
	}
	for k, v := range raw {
		var e Entry
		if err := json.Unmarshal(v, &e); err != nil {
			return nil, fmt.Errorf("invalid entry %q: %w", k, err)
		}
		out[k] = e
	}
	return out, nil
}

func (s FileMapStore) SaveAll(m map[string]Entry) error {
	if m == nil {
		m = map[string]Entry{}
	}
	blob, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return writeJSONFile(s.Path, blob)
}
