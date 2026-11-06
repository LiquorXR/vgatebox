package main

// native.go：Native 后端结构、构造与隧道变更入口。
import (
	"fanout/internal/paths"
	"fmt"
	"sync"
)

// Native 是 fanout 自己运行 Xray 的唯一入口后端。
//
// 入站数据存在 native.json，Xray 的运行配置每次改动后整份重新生成。
// 全量重写比增量改省心：配置是纯函数产物，不会出现改了一半的中间态。
type Native struct {
	mu              sync.Mutex
	dir             string
	store           *nativeStore
	proc            *xrayProc
	committed       *nativeStore
	runtime         nativeRuntime
	transactionHook func(string) error
	unavailable     bool
	// procLive 表示本实例是否已拉起 Xray 且之后没有停掉：
	// 只有“运行配置与磁盘一致且进程是本实例拉起的”才跳过重启。
	// 走文件比较而不是探活：崩溃自愈仍靠“配置变化即重启”与启动恢复。
	// 调用方均持有 n.mu（与 store/committed 一致），读写安全。
	procLive bool
}

func openNative(workDir string) (*Native, error) {
	if workDir == "" {
		return nil, fmt.Errorf("自建模式缺少工作目录")
	}
	bin, err := findXray(workDir)
	if err != nil {
		return nil, err
	}
	proc := &xrayProc{bin: bin, dir: workDir}
	proc.reapOrphan()
	if err := paths.Ensure(workDir); err != nil {
		return nil, err
	}
	if err := recoverConfigTransaction(paths.StateDir(workDir), nativeJournal, nativeConfigFiles); err != nil {
		return nil, err
	}
	store, err := loadNativeStore(workDir)
	if err != nil {
		return nil, err
	}
	n := &Native{
		dir:       workDir,
		store:     store,
		proc:      proc,
		committed: store,
	}
	return n, nil
}

func (n *Native) Kind() string { return "native" }

func (n *Native) Describe() string {
	return "vgatebox"
}

// OnTunnelsChanged 在隧道集合变化后重建配置。自建模式下出站直接由隧道列表
// 推导，所以隧道一变就要重新生成，否则新出口没有对应的 socks 出站。
func (n *Native) OnTunnelsChanged(tunnels []*Tunnel) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.apply(tunnels)
}

// Close 停掉自己拉起的 Xray。
func (n *Native) Close() {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.proc.stop()
	n.procLive = false
}
