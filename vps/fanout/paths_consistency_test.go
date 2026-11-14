package main

import (
	"fanout/internal/paths"
	"reflect"
	"testing"
)

// 落盘文件名有两个写法：包内常量（历史，测试也在用）与 paths 包。
// 在统一目录切换完成、常量删除之前，本测试保证两者不漂移。
func TestPathConstantsMatchPathsPackage(t *testing.T) {
	dir := t.TempDir()
	if statePath(dir) != paths.State(dir) {
		t.Errorf("statePath != paths.State: %q vs %q", statePath(dir), paths.State(dir))
	}
	if nativeStatePath(dir) != paths.Native(dir) {
		t.Errorf("nativeStatePath != paths.Native: %q vs %q", nativeStatePath(dir), paths.Native(dir))
	}
	if webSettingsFilePath(dir) != paths.Settings(dir) {
		t.Errorf("webSettingsFilePath != paths.Settings")
	}
	if stateJournal != paths.StateJournal() {
		t.Errorf("stateJournal = %q, paths = %q", stateJournal, paths.StateJournal())
	}
	if nativeJournal != paths.NativeJournal() {
		t.Errorf("nativeJournal = %q, paths = %q", nativeJournal, paths.NativeJournal())
	}
	if consoleJournal != paths.ConsoleJournal() {
		t.Errorf("consoleJournal = %q, paths = %q", consoleJournal, paths.ConsoleJournal())
	}
	if managementJournal != paths.ManagementJournal() {
		t.Errorf("managementJournal = %q, paths = %q", managementJournal, paths.ManagementJournal())
	}
	if !reflect.DeepEqual(nativeConfigFiles, paths.NativeFiles()) {
		t.Errorf("nativeConfigFiles = %v, paths = %v", nativeConfigFiles, paths.NativeFiles())
	}
	if !reflect.DeepEqual(managementConfigFiles, paths.ManagementFiles()) {
		t.Errorf("managementConfigFiles = %v, paths = %v", managementConfigFiles, paths.ManagementFiles())
	}
	if !reflect.DeepEqual([]string{"state.json"}, paths.StateFiles()) {
		t.Errorf("paths.StateFiles = %v", paths.StateFiles())
	}
	if !reflect.DeepEqual([]string{"console.json"}, paths.ConsoleFiles()) {
		t.Errorf("paths.ConsoleFiles = %v", paths.ConsoleFiles())
	}
}
