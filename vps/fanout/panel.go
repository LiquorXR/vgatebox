package main

import (
	"fanout/internal/paths"
	"fmt"
	"os"
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

// panelManager 持有内置 Xray 后端的显式依赖：工作目录与懒加载实例。
// 之前是包级匿名 struct 全局变量，测试间隐式共享且无法注入；
// 现在默认实例 defaultPanels 保持原有调用方零改动，
// 需要隔离的单测可 NewPanelManager() 自建实例并直接调用其方法。
type panelManager struct {
	mu      sync.Mutex
	current Panel
	workDir string
}

// NewPanelManager 供单测/未来显式注入使用：返回独立面板注册表。
func NewPanelManager() *panelManager { return &panelManager{} }

func (m *panelManager) close() {
	m.mu.Lock()
	p := m.current
	m.mu.Unlock()
	if p != nil {
		p.Close()
	}
}

func (m *panelManager) configure(workDir, mode string) error {
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
	m.mu.Lock()
	m.workDir = workDir
	m.current = nil
	m.mu.Unlock()
	return nil
}

func (m *panelManager) open() (Panel, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.current != nil {
		return m.current, nil
	}
	n, err := openNative(m.workDir)
	if err != nil {
		return nil, err
	}
	m.current = n
	return n, nil
}

// defaultPanels 是生产路径的默认注册表，保持原有全局函数语义。
var defaultPanels = NewPanelManager()

// closePanel releases the backend resources at process exit.
func closePanel() { defaultPanels.close() }

// panelModeFile is the legacy external-backend selection file. It is read only
// to reject an upgrade that would otherwise start native Xray beside an old
// external panel.
func panelModeFile(dir string) string { return paths.PanelMode(dir) }

// configurePanel records the native backend work directory.
//
// External panel modes are no longer supported. Existing native installs and
// the old service units that pass "-panel native" keep working; any other
// value makes startup fail with a migration message instead of silently
// switching to native while the external panel may still own the ports.
func configurePanel(workDir, mode string) error { return defaultPanels.configure(workDir, mode) }

// openPanel returns the built-in Xray backend.
func openPanel() (Panel, error) { return defaultPanels.open() }

// currentPanelMode is kept for native-only state checks. There is no runtime
// mode selection anymore.
func currentPanelMode() string {
	return "native"
}
