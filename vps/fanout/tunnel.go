package main

// tunnel.go：Tunnel 结构、身份标识、凭据与启停状态。
import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"sync"
	"time"
)

// SocksCred 是一条隧道的 SOCKS5 访问凭据。
//
// 每条隧道一套独立凭据：泄露一条不会连累其他出口，
// 换节点时也能只重置这一条而不影响已分发的其他配置。
type SocksCred struct {
	User string `json:"user"`
	Pass string `json:"pass"`
}

// Tunnel 是一条运行中的隧道：一个 netns + 一个 openvpn 进程 + 一个本地 SOCKS5 端口。
type Tunnel struct {
	Slot           int         `json:"slot"`
	Port           int         `json:"port"`
	Node           Node        `json:"node"`
	Status         string      `json:"status"` // starting | up | failed | stopped
	ExitIP         string      `json:"exit_ip"`
	Err            string      `json:"err,omitempty"`
	Since          time.Time   `json:"since"`
	Cred           SocksCred   `json:"cred"`
	Policy         *SlotPolicy `json:"policy,omitempty"`
	previousPolicy *SlotPolicy
	LatencyMS      float64 `json:"latency_ms,omitempty"`

	ns             string
	listener       net.Listener
	ovpn           *exec.Cmd
	mu             sync.Mutex
	opMu           sync.Mutex
	changeMu       sync.Mutex
	workCtx        context.Context
	cancelWork     context.CancelFunc
	clients        map[net.Conn]bool
	cooldown       map[string]time.Time
	validating     bool // permits only the manager's own expected/actual probe
	validationCred *SocksCred
	onProbePhase   func(string)
	probeStage     string
	// Diagnostic candidate checks may skip the optional IP type lookup to
	// avoid bulk provider calls. Production exits never set this.
	skipEgressProfile   bool
	networkIndex        *int
	egress              *EgressProfile
	egressExpectedIP    string
	egressActualIP      string
	egressVerifiedAt    string
	egressNodeHost      string
	egressProfileDigest string
	// swapped 是这条出口换节点时用过的 hostname，按时间先后排。
	// 手动换节点要避开它们：只排除"当前这个"的话，连点两次就会在
	// 两个节点之间来回跳（A 换成 B，B 再换回 A）。
	swapped []string
	// prevHost 是"换节点动作还没收尾"的标记：记着换之前绑的是谁。
	//
	// 换节点分两步——先把隧道连到新节点，再把入站从旧节点改绑过来。
	// 两步之间崩溃或重启的话，存盘的隧道已经是新节点、而入站还指着旧节点，
	// 两边对不上，那个入站就掉成了没人认领的孤儿。
	// 这个字段跟着状态一起落盘，重启后照着它把入站接回来。
	prevHost string
}

// prevHostOf 读"换节点未收尾"标记。
func (t *Tunnel) prevHostOf() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.prevHost
}

// setPrevHost 记下或清掉"换节点未收尾"标记。空串表示已经收尾。
func (t *Tunnel) setPrevHost(host string) {
	t.mu.Lock()
	t.prevHost = host
	t.mu.Unlock()
}

// swapHistoryMax 是换节点历史的上限。
//
// 留太多会把同地区的候选节点排干，反而让自动重连没得选；
// 留太少又挡不住来回跳。一个地区的可用节点通常个位数到十几个，
// 16 条足够覆盖"一轮都换过"的情况。
const swapHistoryMax = 16

// swapAvoid 返回换节点时要跳过的 hostname：历史用过的，加上当前这个。
func (t *Tunnel) swapAvoid() map[string]bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make(map[string]bool, len(t.swapped)+1)
	for _, h := range t.swapped {
		out[h] = true
	}
	if t.Node.HostName != "" {
		out[t.Node.HostName] = true
	}
	return out
}

// rememberSwap 把一个换掉的节点记进历史，超出上限就丢最早的。
func (t *Tunnel) rememberSwap(host string) {
	if host == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, h := range t.swapped {
		if h == host {
			return
		}
	}
	t.swapped = append(t.swapped, host)
	if len(t.swapped) > swapHistoryMax {
		t.swapped = t.swapped[len(t.swapped)-swapHistoryMax:]
	}
}

// forgetSwaps 清空换节点历史。同地区的节点都换过一轮之后要清一次，
// 否则用户再点就只能收到"没有可用节点"。
func (t *Tunnel) forgetSwaps() {
	t.mu.Lock()
	t.swapped = nil
	t.mu.Unlock()
}

// 名字里都带上实例标识，否则同机第二个 fanout 会把这条隧道拆掉（见 instance.go）。
// 默认工作目录下 instTag 是空串、instBase 是 99，名字与老版本完全一致。
func (t *Tunnel) nsName() string { return fmt.Sprintf("fo%s%d", instTag, t.Slot) }

// vethNames 返回母机侧与 netns 侧的网卡名。
// 网卡名上限 15 个字符，"fov" + 4 位标识 + 槽位最多 9 个，留足余量。
func (t *Tunnel) vethNames() (string, string) {
	return fmt.Sprintf("fov%s%d", instTag, t.Slot), fmt.Sprintf("fop%s%d", instTag, t.Slot)
}

// credential 取一份凭据副本，避免读写并发。
func (t *Tunnel) credential() SocksCred {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.Cred
}

func (t *Tunnel) probeCredential() SocksCred {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.validating && t.validationCred != nil {
		return *t.validationCred
	}
	return t.Cred
}

// setCredential 换掉这条隧道的 SOCKS5 凭据。已建立的连接不受影响，
// 新连接立即按新凭据校验。
func (t *Tunnel) setCredential(c SocksCred) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.Cred = c
}

// probeExitIP 通过隧道查询出口 IP，用于确认这条隧道确实换了 IP。
func (t *Tunnel) probeExitIP() (string, error) {
	return t.probeExpectedExitIP()
}

func (t *Tunnel) setValidating(v bool) {
	t.mu.Lock()
	t.validationCred = nil
	t.validating = false
	if v && (t.workCtx == nil || t.workCtx.Err() == nil) {
		if cred, err := newSocksCred(); err == nil {
			t.validationCred = &cred
			t.validating = true
		}
	}
	t.mu.Unlock()
}

// stop 停止这条隧道并清理它占用的所有资源。
func (t *Tunnel) stop() {
	t.stopWithCleanup(t.teardownNetns)
}

func (t *Tunnel) stopWithCleanup(cleanup func()) {
	t.blockWork()
	t.opMu.Lock()
	defer t.opMu.Unlock()
	t.mu.Lock()
	t.Status = "stopped"
	t.mu.Unlock()
	if t.listener != nil {
		t.listener.Close()
		t.listener = nil
	}
	if t.ovpn != nil && t.ovpn.Process != nil {
		_ = t.ovpn.Process.Kill()
		t.ovpn = nil
	}
	cleanup()
}
