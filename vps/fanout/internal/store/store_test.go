package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
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
