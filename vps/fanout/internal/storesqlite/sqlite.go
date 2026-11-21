// Package storesqlite 是 store 接口的 SQLite 后端（modernc.org/sqlite，纯 Go，
// CGO_ENABLED=0 可构建）。与 internal/store 的 JSON 文件后端实现同一接口，
// 切换只改构造处，调用方不动。
//
// 设计取舍：
//   - 单库多域：docs（单文档）与 entries（store+key 寻址）两张表，
//     checks/ip_type/slot_health 共用一个库文件。
//   - 单连接（SetMaxOpenConns(1)）：管理器本就是单写者，写操作毫秒级；
//     避免多连接下的 PRAGMA 漂移与 busy 重试。
//   - 文件 0600：先 OpenFile 建空文件定权限，再 sql.Open（SQLite 建文件
//     会走 umask，直接建是 0644，不合私有要求）。
//   - WAL + synchronous=NORMAL + busy_timeout：崩溃恢复靠 WAL，
//     备份前调 Checkpoint（见 DB.Checkpoint，供 installer 调用）。
package storesqlite

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"fanout/internal/store"

	_ "modernc.org/sqlite"
)

// DB 是一库文件的句柄。并发安全（驱动层串行 + 单连接）。
type DB struct {
	db *sql.DB
}

// Open 打开（不存在则按 0600 创建）并迁移表结构。
func Open(path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("%s: %w", pragma, err)
		}
	}
	d := &DB{db: db}
	if err := d.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return d, nil
}

// Checkpoint 把 WAL 并回主库：备份/卸载前调用，保证单文件自包含。
func (d *DB) Checkpoint() error {
	_, err := d.db.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
	return err
}

func (d *DB) Close() error { return d.db.Close() }

func (d *DB) migrate() error {
	_, err := d.db.Exec(`CREATE TABLE IF NOT EXISTS docs(
		name TEXT PRIMARY KEY, data TEXT NOT NULL, updated_at TEXT NOT NULL)`)
	if err != nil {
		return err
	}
	_, err = d.db.Exec(`CREATE TABLE IF NOT EXISTS entries(
		store TEXT NOT NULL, key TEXT NOT NULL, data TEXT NOT NULL,
		checked_at TEXT NOT NULL, expires_at TEXT NOT NULL,
		PRIMARY KEY(store, key))`)
	return err
}

func formatTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func parseTime(s string) (time.Time, error) { return time.Parse(time.RFC3339Nano, s) }

// DocStore 实现 store.DocStore：缺失文档读失败（与文件后端一致）。
type docStore struct {
	db   *sql.DB
	name string
}

// DocStore 返回命名单文档存储。
func (d *DB) DocStore(name string) store.DocStore { return docStore{db: d.db, name: name} }

func (s docStore) Load() (json.RawMessage, error) {
	var data string
	err := s.db.QueryRow(`SELECT data FROM docs WHERE name = ?`, s.name).Scan(&data)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}

func (s docStore) Save(blob json.RawMessage) error {
	if !json.Valid(blob) {
		return fmt.Errorf("refusing to persist invalid JSON")
	}
	_, err := s.db.Exec(`INSERT INTO docs(name, data, updated_at) VALUES(?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET data=excluded.data, updated_at=excluded.updated_at`,
		s.name, string(blob), formatTime(time.Now()))
	return err
}

// MapStore 实现 store.MapStore：SaveAll 是整表替换（DELETE + INSERT 一事务），
// 对应 flushChecks 的合并写语义。
type mapStore struct {
	db   *sql.DB
	name string
}

// MapStore 返回命名记录集存储。
func (d *DB) MapStore(name string) store.MapStore { return mapStore{db: d.db, name: name} }

func (s mapStore) LoadAll() (map[string]store.Entry, error) {
	out := map[string]store.Entry{}
	rows, err := s.db.Query(`SELECT key, data, checked_at, expires_at FROM entries WHERE store = ?`, s.name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var key, data, checked, expires string
		if err := rows.Scan(&key, &data, &checked, &expires); err != nil {
			return nil, err
		}
		checkedAt, err := parseTime(checked)
		if err != nil {
			return nil, fmt.Errorf("invalid checked_at for %q: %w", key, err)
		}
		expiresAt, err := parseTime(expires)
		if err != nil {
			return nil, fmt.Errorf("invalid expires_at for %q: %w", key, err)
		}
		out[key] = store.Entry{Data: json.RawMessage(data), CheckedAt: checkedAt, ExpiresAt: expiresAt}
	}
	return out, rows.Err()
}

func (s mapStore) SaveAll(m map[string]store.Entry) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// 空表与超大表（>2000）走全量替换：先清空再插入，避免超长 NOT IN。
	// 常规批量（1..2000）走 UPSERT + 孤儿键清理，减少 WAL 膨胀与索引抖动，
	// 同时保持与旧语义一致的整表替换效果。
	if len(m) == 0 || len(m) > 2000 {
		if _, err := tx.Exec(`DELETE FROM entries WHERE store = ?`, s.name); err != nil {
			return err
		}
	}
	// 预编译复用：批量检测每 2s 全量落盘时，避免每次循环重复 Prepare。
	stmt, err := tx.Prepare(`INSERT INTO entries(store, key, data, checked_at, expires_at)
		VALUES(?, ?, ?, ?, ?)
		ON CONFLICT(store, key) DO UPDATE SET data=excluded.data, checked_at=excluded.checked_at, expires_at=excluded.expires_at`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for key, e := range m {
		if _, err := stmt.Exec(s.name, key, string(e.Data), formatTime(e.CheckedAt), formatTime(e.ExpiresAt)); err != nil {
			return err
		}
	}
	if len(m) > 0 && len(m) <= 2000 {
		// 构造 NOT IN 清理语句：keys 数量受 maxStoredCheckResults(1000) 约束，
		// 占位符数量安全可控。
		args := make([]any, 0, len(m)+1)
		args = append(args, s.name)
		placeholders := make([]string, 0, len(m))
		for key := range m {
			args = append(args, key)
			placeholders = append(placeholders, "?")
		}
		// strings/strings.Builder 避免 fmt 拼接 SQL 模板。
		query := `DELETE FROM entries WHERE store = ? AND key NOT IN (` + stringJoin(placeholders, ",") + `)`
		if _, err := tx.Exec(query, args...); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func stringJoin(parts []string, sep string) string {
	if len(parts) == 0 {
		return ""
	}
	n := len(sep) * (len(parts) - 1)
	for _, p := range parts {
		n += len(p)
	}
	buf := make([]byte, 0, n)
	for i, p := range parts {
		if i > 0 {
			buf = append(buf, sep...)
		}
		buf = append(buf, p...)
	}
	return string(buf)
}
