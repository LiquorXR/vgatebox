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
//
// Status 语义：starting（建链中）| up（健康出网）| degraded（仍可出网，
// 延迟超 Slot 上限被观察中，不断流）| blocked（证据失效已阻断，不断流但拒新连接，
// 等重连）| waiting_for_nodes（无候选等重试）| failed | stopped。
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
	// latencyInstantMS 是最近一次探活的原始延迟；LatencyMS 是 EWMA 平滑值，
	// 展示、排序与门限统一用平滑值，单次毛刺不再直接触发阻断。
	latencyInstantMS float64

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
	// reconnectAt 记最近 10 分钟内每次重连进入的时间点，用于抖动降频。
	// 只存内存，重启清零；手动 Resume/Swap/改策略视为 operator 意图，清零。
	reconnectAt []time.Time
}

// servingStatus 隧道是否在出网服务：up 与 degraded 都接受新连接，
// 区别只在 degraded 正被健康检查观察、持续不达标会升级为重连。
func servingStatus(s string) bool { return s == "up" || s == "degraded" }

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

const (
	// flapWindow 是抖动统计窗口：窗口内重连达到 flapThreshold 即判 flapping。
	flapWindow = 10 * time.Minute
	// flapThreshold 是判 flapping 的重连次数。
	flapThreshold = 5
	// flapSlowMin/flapSlowMax 是降频后的退避区间：从 60s 起步，上限 5 分钟，
	// 避免对着一批死节点空转。对照默认的 5s～60s（见 manager_lifecycle.go）。
	flapSlowMin = 60 * time.Second
	flapSlowMax = 5 * time.Minute
	// ewmaAlpha 是延迟平滑系数：新样本占 4 成，历史占 6 成。
	// 单次毛刺约 3 轮收敛，持续变慢约 4 轮把平滑值推过门限。
	ewmaAlpha = 0.4
)

// recordReconnect 记一次重连进入，返回窗口内的累计次数。
func (t *Tunnel) recordReconnect() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	kept := t.reconnectAt[:0]
	for _, at := range t.reconnectAt {
		if now.Sub(at) <= flapWindow {
			kept = append(kept, at)
		}
	}
	kept = append(kept, now)
	t.reconnectAt = kept
	return len(kept)
}

// clearFlap 清掉抖动计数。手动 Resume/Swap/改策略后调用。
func (t *Tunnel) clearFlap() {
	t.mu.Lock()
	t.reconnectAt = nil
	t.mu.Unlock()
}

// flapSlow 判断是否处于降频状态：窗口内重连达标且最近一次在 5 分钟内。
// 隧道持续稳定后自然解除（下次 recordReconnect 会把旧记录裁掉）。
func (t *Tunnel) flapSlow() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.reconnectAt) < flapThreshold {
		return false
	}
	return time.Since(t.reconnectAt[len(t.reconnectAt)-1]) <= flapSlowMax
}

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
