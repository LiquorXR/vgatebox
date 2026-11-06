package main

import (
	"encoding/json"
	"fanout/internal/paths"
	"os"
	"path/filepath"
	"testing"
)

func TestPersistedSchemaVersionBackwardCompat(t *testing.T) {
	// 缺字段 = legacy，必须放行。
	if _, err := decodePersistedState([]byte(`{"tunnels":[]}`), 32); err != nil {
		t.Fatalf("legacy state rejected: %v", err)
	}
	if _, err := decodeNativeStore([]byte(`{"next_id":1,"inbounds":[]}`)); err != nil {
		t.Fatalf("legacy native rejected: %v", err)
	}
	p := ExitPlan{Count: 0}
	if err := validateExitPlan(&p, 32); err != nil {
		t.Fatalf("legacy plan rejected: %v", err)
	}
	// 当前版本放行。
	if _, err := decodePersistedState([]byte(`{"schema_version":1,"tunnels":[]}`), 32); err != nil {
		t.Fatalf("v1 state rejected: %v", err)
	}
	if _, err := decodeNativeStore([]byte(`{"schema_version":1,"next_id":1,"inbounds":[]}`)); err != nil {
		t.Fatalf("v1 native rejected: %v", err)
	}
	p1 := ExitPlan{SchemaVersion: 1, Count: 0}
	if err := validateExitPlan(&p1, 32); err != nil {
		t.Fatalf("v1 plan rejected: %v", err)
	}
	// 未来版本 fail-closed。
	if _, err := decodePersistedState([]byte(`{"schema_version":2,"tunnels":[]}`), 32); err == nil {
		t.Fatal("future state accepted")
	}
	if _, err := decodeNativeStore([]byte(`{"schema_version":99,"next_id":1,"inbounds":[]}`)); err == nil {
		t.Fatal("future native accepted")
	}
	pf := ExitPlan{SchemaVersion: 2, Count: 0}
	if err := validateExitPlan(&pf, 32); err == nil {
		t.Fatal("future plan accepted")
	}
	if err := validateExitPlan(nil, 32); err == nil {
		t.Fatal("nil plan accepted")
	}
}

func TestMigrationCheckRejectsFutureSchemaVersion(t *testing.T) {
	for _, tc := range []struct{ file, content string }{
		{"state.json", `{"schema_version":2,"tunnels":[]}`},
		{"native.json", `{"schema_version":2,"next_id":1,"inbounds":[]}`},
		{"console.json", `{"schema_version":2,"count":0}`},
	} {
		t.Run(tc.file, func(t *testing.T) {
			dir := migrationFixture(t)
			if err := os.WriteFile(filepath.Join(paths.StateDir(dir), tc.file), []byte(tc.content), 0600); err != nil {
				t.Fatal(err)
			}
			report := checkMigrationState(paths.StateDir(dir), 32, 8899)
			if report.ConfigValid {
				t.Fatalf("future %s accepted: %+v", tc.file, report)
			}
			found := false
			for _, is := range report.Issues {
				if is.File == tc.file {
					found = true
				}
			}
			if !found {
				t.Fatalf("no issue for %s: %+v", tc.file, report.Issues)
			}
		})
	}
	// legacy（缺字段）仍放行：v031 夹具本身就是无版本的。
	dir := migrationFixture(t)
	if report := checkMigrationState(paths.StateDir(dir), 3, 8899); !report.ConfigValid {
		t.Fatalf("legacy fixture rejected: %+v", report.Issues)
	}
}

func TestPersistedSchemaVersionWritten(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(4, dir)
	m.tunnels[1] = &Tunnel{Slot: 1, Port: 17928, Status: "stopped", Node: Node{HostName: "a"}}
	if err := m.saveState(); err != nil {
		t.Fatal(err)
	}
	blob, err := os.ReadFile(statePath(dir))
	if err != nil {
		t.Fatal(err)
	}
	var st persistedState
	if err := json.Unmarshal(blob, &st); err != nil {
		t.Fatal(err)
	}
	if st.SchemaVersion != persistedSchemaVersion {
		t.Fatalf("state schema_version = %d, want %d", st.SchemaVersion, persistedSchemaVersion)
	}

	n := &Native{dir: dir, store: &nativeStore{NextID: 1}}
	if err := n.store.save(dir); err != nil {
		t.Fatal(err)
	}
	blob, err = os.ReadFile(nativeStatePath(dir))
	if err != nil {
		t.Fatal(err)
	}
	var ns nativeStore
	if err := json.Unmarshal(blob, &ns); err != nil {
		t.Fatal(err)
	}
	if ns.SchemaVersion != persistedSchemaVersion {
		t.Fatalf("native schema_version = %d, want %d", ns.SchemaVersion, persistedSchemaVersion)
	}

	c, err := newConsole(NewManager(4, dir))
	if err != nil {
		t.Fatal(err)
	}
	defer c.shutdown()
	if err := c.savePlan(ExitPlan{Count: 0}); err != nil {
		t.Fatal(err)
	}
	blob, err = os.ReadFile(paths.Console(dir))
	if err != nil {
		t.Fatal(err)
	}
	var p ExitPlan
	if err := json.Unmarshal(blob, &p); err != nil {
		t.Fatal(err)
	}
	if p.SchemaVersion != persistedSchemaVersion {
		t.Fatalf("console schema_version = %d, want %d", p.SchemaVersion, persistedSchemaVersion)
	}
}
