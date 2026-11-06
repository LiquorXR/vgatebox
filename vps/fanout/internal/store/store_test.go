package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFileDocStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "doc.json")
	// 父目录不存在时 loud 失败：调用方（loader/Ensure）负责建目录，
	// store 层不静默建目录，避免拼错路径时满地建目录。
	if err := (FileDocStore{Path: path}).Save(json.RawMessage(`{"a":1}`)); err == nil {
		t.Fatal("missing parent directory should fail loudly")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	s := FileDocStore{Path: path}
	if err := s.Save(json.RawMessage(`{"a":1}`)); err != nil {
		t.Fatal(err)
	}
	got, err := s.Load()
	if err != nil || string(got) != `{"a":1}` {
		t.Fatalf("round trip = %q, %v", got, err)
	}
	if err := s.Save(json.RawMessage(`{broken`)); err == nil {
		t.Fatal("invalid JSON must be refused")
	}
	if _, err := (FileDocStore{Path: filepath.Join(t.TempDir(), "missing.json")}).Load(); err == nil {
		t.Fatal("missing document should fail")
	}
}

func TestFileMapStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "map.json")
	s := FileMapStore{Path: path}
	m, err := s.LoadAll()
	if err != nil || len(m) != 0 {
		t.Fatalf("missing map should load empty: %v %v", m, err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	in := map[string]Entry{
		"h1": {Data: json.RawMessage(`{"x":1}`), CheckedAt: now},
		"h2": {Data: json.RawMessage(`[1,2]`)},
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
	if !got["h2"].ExpiresAt.IsZero() {
		t.Fatalf("zero time must survive: %+v", got["h2"])
	}
	if err := s.SaveAll(nil); err != nil {
		t.Fatal(err)
	}
	if got, err := s.LoadAll(); err != nil || len(got) != 0 {
		t.Fatalf("nil save should clear: %v %v", got, err)
	}
}
