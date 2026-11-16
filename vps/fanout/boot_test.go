package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

func TestParseBootConfigDefaults(t *testing.T) {
	cfg, code, ok := parseBootConfig(nil, io.Discard)
	if !ok || code != 0 {
		t.Fatalf("parseBootConfig(nil) = code %d, ok %v; want 0, true", code, ok)
	}
	if cfg.webPort != 8899 {
		t.Errorf("webPort = %d; want 8899", cfg.webPort)
	}
	if cfg.maxSlots != maximumSlots {
		t.Errorf("maxSlots = %d; want %d", cfg.maxSlots, maximumSlots)
	}
	if cfg.workDir != "/var/lib/vgatebox/fanout" {
		t.Errorf("workDir = %q; want default", cfg.workDir)
	}
	if cfg.panelMode != "" || cfg.publicIP != "" {
		t.Errorf("panel/publicIP = %q/%q; want empty", cfg.panelMode, cfg.publicIP)
	}
	if cfg.showVersion || cfg.checkState || cfg.portExplicit {
		t.Errorf("derived flags not zero: %+v", cfg)
	}
}

func TestParseBootConfigExplicitFlags(t *testing.T) {
	cfg, code, ok := parseBootConfig([]string{
		"-web", "19000",
		"-max", "12",
		"-dir", "/tmp/vgatebox-test",
		"-panel", "native",
		"-ip", "203.0.113.7",
	}, io.Discard)
	if !ok || code != 0 {
		t.Fatalf("parseBootConfig = code %d, ok %v; want 0, true", code, ok)
	}
	if cfg.webPort != 19000 || cfg.maxSlots != 12 || cfg.workDir != "/tmp/vgatebox-test" {
		t.Errorf("basic flags not parsed: %+v", cfg)
	}
	if cfg.panelMode != "native" || cfg.publicIP != "203.0.113.7" {
		t.Errorf("panel/publicIP not parsed: %+v", cfg)
	}
	if !cfg.portExplicit {
		t.Error("portExplicit = false; want true for explicit -web")
	}
}

func TestParseBootConfigPortExplicitOnlyWhenVisited(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want bool
	}{
		{name: "default", args: nil, want: false},
		{name: "other flags", args: []string{"-max", "3"}, want: false},
		{name: "web explicit", args: []string{"-web", "8899"}, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, _, ok := parseBootConfig(tc.args, io.Discard)
			if !ok {
				t.Fatal("parseBootConfig unexpectedly failed")
			}
			if cfg.portExplicit != tc.want {
				t.Errorf("portExplicit = %v; want %v", cfg.portExplicit, tc.want)
			}
		})
	}
}

func TestParseBootConfigMaximumSentinel(t *testing.T) {
	cfg, _, ok := parseBootConfig([]string{"-max", "12"}, io.Discard)
	if !ok || cfg.maxSlots != 12 {
		t.Fatalf("max override failed: %+v, ok=%v", cfg, ok)
	}
}

func TestParseBootConfigRejectsUnknownFlag(t *testing.T) {
	var stderr bytes.Buffer
	_, code, ok := parseBootConfig([]string{"-does-not-exist"}, &stderr)
	if ok {
		t.Fatal("unknown flag accepted")
	}
	if code != 2 {
		t.Errorf("exit code = %d; want 2", code)
	}
	if !strings.Contains(stderr.String(), "does-not-exist") {
		t.Errorf("stderr does not mention the unknown flag: %q", stderr.String())
	}
}

func TestParseBootConfigHelp(t *testing.T) {
	var stderr bytes.Buffer
	_, code, ok := parseBootConfig([]string{"-h"}, &stderr)
	if ok {
		t.Fatal("-h reported ok; caller should return the help exit code")
	}
	if code != 0 {
		t.Errorf("help exit code = %d; want 0", code)
	}
}

func TestValidateBootConfig(t *testing.T) {
	for _, tc := range []struct {
		name     string
		maxSlots int
		want     int
	}{
		{name: "zero", maxSlots: 0, want: 1},
		{name: "negative", maxSlots: -1, want: 1},
		{name: "too large", maxSlots: maximumSlots + 1, want: 1},
		{name: "minimum", maxSlots: 1, want: 0},
		{name: "default", maxSlots: maximumSlots, want: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := bootConfig{maxSlots: tc.maxSlots}
			if got := validateBootConfig(&cfg); got != tc.want {
				t.Errorf("validateBootConfig(max=%d) = %d; want %d", tc.maxSlots, got, tc.want)
			}
		})
	}
}

func TestValidateBootConfigPublicIPFallback(t *testing.T) {
	t.Setenv("FANOUT_PUBLIC_IP", "198.51.100.9")
	cfg := bootConfig{maxSlots: 3}
	if code := validateBootConfig(&cfg); code != 0 {
		t.Fatalf("validateBootConfig = %d; want 0", code)
	}
	if cfg.publicIP != "198.51.100.9" {
		t.Errorf("publicIP = %q; want env fallback", cfg.publicIP)
	}

	cfg = bootConfig{maxSlots: 3, publicIP: "203.0.113.5"}
	if code := validateBootConfig(&cfg); code != 0 {
		t.Fatalf("validateBootConfig = %d; want 0", code)
	}
	if cfg.publicIP != "203.0.113.5" {
		t.Errorf("explicit publicIP overwritten: %q", cfg.publicIP)
	}
}

func TestRunFanoutVersionExitsBeforePrivilegeCheck(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runFanout([]string{"-version"}, &stdout, &stderr); code != 0 {
		t.Fatalf("runFanout(-version) = %d; want 0 (stderr: %s)", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "vgatebox") {
		t.Errorf("version output = %q; want vgatebox prefix", stdout.String())
	}
}

func TestRunFanoutRejectsInvalidCapacity(t *testing.T) {
	for _, args := range [][]string{
		{"-max", "0"},
		{"-max", "-1"},
		{"-max", "999999"},
	} {
		if code := runFanout(args, io.Discard, io.Discard); code != 1 {
			t.Errorf("runFanout(%v) = %d; want 1", args, code)
		}
	}
}

func TestRunFanoutCheckStateDoesNotRequireRoot(t *testing.T) {
	// -check-state must return before the root check, exactly like the old
	// startup path. A missing state directory is a reported failure, not a panic.
	dir := t.TempDir()
	if code := runFanout([]string{"-dir", dir, "-check-state", "-max", "2"}, io.Discard, io.Discard); code != 1 {
		t.Errorf("runFanout(-check-state on empty dir) = %d; want 1", code)
	}
}

func TestBootPhaseOrderInRunFanout(t *testing.T) {
	// runFanout is an orchestrator; this source-level guard catches a phase
	// being moved before the storage/privilege step it depends on.
	blob, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	src := string(blob)
	last := -1
	for _, phase := range []string{
		"parseBootConfig",
		"validateBootConfig",
		"bootstrapHost",
		"initControl",
		"startBackground",
		"installShutdownHandler",
		"initHTTP",
		"serveUntilSignal",
	} {
		at := strings.Index(src, phase)
		if at < 0 {
			t.Fatalf("runFanout missing phase %s", phase)
		}
		if at <= last {
			t.Fatalf("phase %s appears out of order", phase)
		}
		last = at
	}
}
