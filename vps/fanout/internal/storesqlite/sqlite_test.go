package storesqlite

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"fanout/internal/store"
)

func openTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "sub", "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestOpenCreatesPrivateFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vgatebox.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if runtime.GOOS != "windows" {
		if st, err := os.Stat(path); err != nil || st.Mode().Perm() != 0600 {
			t.Fatalf("库文件权限不足 0600: %v %v", st, err)
		}
	}
	// 幂等：已存在直接打开。
	db2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	db2.Close()
}

func TestDocStoreRoundTrip(t *testing.T) {
	db := openTestDB(t)
	s := db.DocStore("slot_health")
	if _, err := s.Load(); err == nil {
		t.Fatal("missing document should fail")
	}
	if err := s.Save(json.RawMessage(`{"slots":[]}`)); err != nil {
		t.Fatal(err)
	}
	got, err := s.Load()
	if err != nil || string(got) != `{"slots":[]}` {
		t.Fatalf("round trip = %q, %v", got, err)
	}
	if err := s.Save(json.RawMessage(`{broken`)); err == nil {
		t.Fatal("invalid JSON must be refused")
	}
	// 同库多域互不干扰。
	if err := db.DocStore("other").Save(json.RawMessage(`1`)); err != nil {
		t.Fatal(err)
	}
	if got, err := db.DocStore("other").Load(); err != nil || string(got) != `1` {
		t.Fatalf("second domain = %q, %v", got, err)
	}
}

func TestMapStoreReplaceAll(t *testing.T) {
	db := openTestDB(t)
	s := db.MapStore("checks")
	now := time.Now().UTC().Truncate(time.Second)
	in := map[string]store.Entry{
		"h1": {Data: json.RawMessage(`{"x":1}`), CheckedAt: now},
		"h2": {Data: json.RawMessage(`[1,2]`), ExpiresAt: now.Add(time.Hour)},
	}
	if err := s.SaveAll(in); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadAll()
	if err != nil || len(got) != 2 {
		t.Fatalf("reload = %v, %v", got, err)
	}
	if string(got["h1"].Data) != `{"x":1}` || !got["h1"].CheckedAt.Equal(now) {
		t.Fatalf("entry mismatch: %+v", got["h1"])
	}
	if !got["h2"].ExpiresAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("expiry mismatch: %+v", got["h2"])
	}
	// 整表替换：旧键消失。
	if err := s.SaveAll(map[string]store.Entry{}); err != nil {
		t.Fatal(err)
	}
	if got, err := s.LoadAll(); err != nil || len(got) != 0 {
		t.Fatalf("clear = %v %v", got, err)
	}
	// 同库另一域不受影响。
	other := db.MapStore("ippure")
	if err := other.SaveAll(map[string]store.Entry{"k": {Data: json.RawMessage(`{}`)}}); err != nil {
		t.Fatal(err)
	}
	if got, err := s.LoadAll(); err != nil || len(got) != 0 {
		t.Fatalf("cross-domain leak: %v %v", got, err)
	}
}

func TestCheckpointKeepsSingleFileUsable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.DocStore("d").Save(json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	if err := db.Checkpoint(); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	if got, err := db2.DocStore("d").Load(); err != nil || string(got) != `{}` {
		t.Fatalf("reopen = %q, %v", got, err)
	}
}
