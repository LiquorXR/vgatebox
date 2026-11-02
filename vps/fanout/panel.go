package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Panel is the node-link backend used by fanout.
//
// Only the built-in Xray backend (Native) remains. The interface is kept as a
// narrow seam for tests and for callers that should not depend on the concrete
// type.
type Panel interface {
	Kind() string
	Describe() string

	Inbounds(live map[string]bool) ([]Inbound, error)
	InboundDetail(id int, publicHost string) (*InboundDetail, error)
	InboundLinks(ids []int, publicHost string) ([]string, error)

	Bind(inboundTag string, hostname string, tunnels []*Tunnel) error
	Rebind(oldHost string, target *Tunnel, tunnels []*Tunnel) error
	ResyncOutbound(t *Tunnel, tunnels []*Tunnel) error

	CloneToTunnels(templateID int, hosts []string, tunnels []*Tunnel) ([]int, error)
	DeleteInbounds(ids []int, tunnels []*Tunnel) error

	CreateInbound(spec NewInboundSpec, tunnels []*Tunnel) (*CreatedInbound, error)

	UpdateInbound(id int, patch InboundPatch, tunnels []*Tunnel) error

	AddClient(id int, email string, tunnels []*Tunnel) error
	DeleteClient(id int, email string, tunnels []*Tunnel) error
	ResetClient(id int, email string, tunnels []*Tunnel) error

	OnTunnelsChanged(tunnels []*Tunnel) error

	Close()
}

// InboundPatch describes a partial inbound update. A nil field is unchanged.
type InboundPatch struct {
	Port     *int
	Remark   *string
	NameMode *string
	Enable   *bool
}

// CreatedInbound is the summary returned after creating an inbound.
type CreatedInbound struct {
	ID       int    `json:"id"`
	Port     int    `json:"port"`
	Protocol string `json:"protocol"`
	Remark   string `json:"remark"`
	Network  string `json:"network"`
	Security string `json:"security"`
}

// closePanel releases the backend resources at process exit.
func closePanel() {
	panelState.mu.Lock()
	p := panelState.current
	panelState.mu.Unlock()
	if p != nil {
		p.Close()
	}
}

var panelState struct {
	mu      sync.Mutex
	current Panel
	workDir string
}

// panelModeFile is the legacy external-backend selection file. It is read only
// to reject an upgrade that would otherwise start native Xray beside an old
// external panel.
func panelModeFile(dir string) string { return filepath.Join(dir, "panel_mode") }

// configurePanel records the native backend work directory.
//
// External panel modes are no longer supported. Existing native installs and
// the old service units that pass "-panel native" keep working; any other
// value makes startup fail with a migration message instead of silently
// switching to native while the external panel may still own the ports.
func configurePanel(workDir, mode string) error {
	mode = strings.TrimSpace(mode)
	if mode != "" && mode != "native" {
		return fmt.Errorf("外部入口后端 %q 已移除；只支持 native（内置 Xray）", mode)
	}
	blob, err := os.ReadFile(panelModeFile(workDir))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("读取旧入口后端配置失败: %w", err)
	}
	if err == nil {
		stale := strings.TrimSpace(string(blob))
		if stale != "" && stale != "native" {
			return fmt.Errorf("检测到旧的外部入口后端 %q；请先停止外部面板并删除 %s 后再升级", stale, panelModeFile(workDir))
		}
	}
	panelState.mu.Lock()
	panelState.workDir = workDir
	panelState.current = nil
	panelState.mu.Unlock()
	return nil
}

// openPanel returns the built-in Xray backend.
func openPanel() (Panel, error) {
	panelState.mu.Lock()
	defer panelState.mu.Unlock()
	if panelState.current != nil {
		return panelState.current, nil
	}
	n, err := openNative(panelState.workDir)
	if err != nil {
		return nil, err
	}
	panelState.current = n
	return n, nil
}

// currentPanelMode is kept for native-only state checks. There is no runtime
// mode selection anymore.
func currentPanelMode() string {
	return "native"
}
