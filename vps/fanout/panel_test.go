package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigurePanelOnlyAcceptsNative(t *testing.T) {
	dir := t.TempDir()
	t.Cleanup(func() {
		panelState.mu.Lock()
		panelState.current = nil
		panelState.workDir = ""
		panelState.mu.Unlock()
	})

	if err := configurePanel(dir, "native"); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"3x-ui", "xray-cf-lite"} {
		if err := configurePanel(dir, mode); err == nil {
			t.Fatalf("external mode %q was accepted", mode)
		}
	}

	// A stale selection must fail even when the service unit still passes
	// "-panel native"; otherwise the external panel can keep owning the ports.
	if err := os.WriteFile(filepath.Join(dir, "panel_mode"), []byte("3x-ui"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := configurePanel(dir, "native"); err == nil {
		t.Fatal("stale external panel_mode was accepted")
	}

	if err := os.WriteFile(filepath.Join(dir, "panel_mode"), []byte("native"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := configurePanel(dir, ""); err != nil {
		t.Fatalf("native panel_mode rejected: %v", err)
	}
	if got := currentPanelMode(); got != "native" {
		t.Fatalf("currentPanelMode() = %q", got)
	}
}
