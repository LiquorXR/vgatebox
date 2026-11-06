package main

// manager_selectors.go：Manager 的只读选择器（节点/隧道快照、槽位分配、候选过滤）。
import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// RefreshNodes 重新拉取节点列表。
func (m *Manager) RefreshNodes() (int, error) {
	nodes, err := fetchNodes(60 * time.Second)
	if err != nil {
		return 0, err
	}
	m.mu.Lock()
	m.nodes = nodes
	m.fetched = time.Now()
	m.mu.Unlock()
	// 候选池内容变化是 apiConsole 可见变更（candidates/regions），递增版本号。
	// 只有池文件 mtime 变化（WatchNodePool）或显式刷新才会走到这里，无空转递增。
	if m.console != nil {
		m.console.bumpConsoleVersion()
	}
	// Never wait for plan changes from a reconnect worker: deleting a Slot may
	// be waiting for that worker to finish. A buffered wake coalesces refreshes.
	if m.console != nil {
		select {
		case m.console.poolChanged <- struct{}{}:
		default:
		}
	}
	return len(nodes), nil
}

func (m *Manager) Nodes() ([]Node, time.Time) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Node, len(m.nodes))
	copy(out, m.nodes)
	for i, n := range out {
		if local, ok := m.localEvidence[n.HostName]; ok && local.Config == n.Config && freshCandidateImage(local) != nil {
			out[i] = local
		}
		if out[i].OpenVPNEgress != nil && freshCandidateImage(out[i]) == nil {
			out[i].IPType, out[i].ISP, out[i].ASN, out[i].ExpectedExitIP = "unknown", "", "", ""
			out[i].Residential = false
			out[i].OpenVPNStatus = "stale"
			// SourceResidential 是来源标签，不随出口画像过期而改变。
		}
	}
	return out, m.fetched
}

// NodeCount avoids the full copy and evidence merge when only pool size matters
// (TTL estimation runs for every stored result).
func (m *Manager) NodeCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.nodes)
}

func (m *Manager) Tunnels() []*Tunnel {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Tunnel, 0, len(m.tunnels))
	for _, t := range m.tunnels {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slot < out[j].Slot })
	return out
}

// freeSlot 找一个未占用的槽位。槽位同时决定端口与网段。
func (m *Manager) freeSlot() (int, error) {
	for i := 1; i <= m.maxSlots; i++ {
		if _, used := m.tunnels[i]; !used {
			return i, nil
		}
	}
	return 0, fmt.Errorf("槽位已满（上限 %d）", m.maxSlots)
}

// candidatesFor 以这条隧道当前的节点打头，后面跟上同地区的其他节点作为备选。
//
// 打头的一定是 t.Node：自动重连的目标是把这条出口恢复原样，先试原节点。
// 备选会避开用户手动换掉过的节点——那些是他明确不想要的 IP，
// 让重连悄悄换回去等于撤销了他的操作。
func (m *Manager) candidatesFor(t *Tunnel) []Node {
	policy := t.policySnapshot()
	t.mu.Lock()
	first := t.Node
	t.mu.Unlock()
	avoid := t.swapAvoid()
	out := []Node{}
	eligible := func(n Node) bool {
		t.mu.Lock()
		until := t.cooldown[n.HostName]
		t.mu.Unlock()
		if time.Now().Before(until) {
			return false
		}
		image := freshCandidateImage(n)
		if image != nil {
			return policy.checkImage(*image) == nil
		}
		return policy.Country == "" || n.CountryCode == "" || strings.EqualFold(policy.Country, n.CountryCode)
	}
	if first.HostName != "" && !m.nodeInUse(first.HostName, t.Slot) && eligible(first) {
		out = append(out, first)
	}
	candidates := m.policyCandidates(policy, avoid, t.Slot)
	t.mu.Lock()
	failedUntil := make(map[string]time.Time, len(t.cooldown))
	for host, until := range t.cooldown {
		failedUntil[host] = until
	}
	t.mu.Unlock()
	// A round can outlast the cooldown. Keep untried candidates ahead of
	// expired failures so the six-probe budget cannot starve the rest of the
	// pool by retrying the same highest-ranked failures every round.
	sort.SliceStable(candidates, func(i, j int) bool {
		return failedUntil[candidates[i].HostName].Before(failedUntil[candidates[j].HostName])
	})
	for _, n := range candidates {
		if len(out) >= 6 {
			break
		}
		if n.HostName != first.HostName && eligible(n) {
			out = append(out, n)
		}
	}
	return out
}

func (m *Manager) exportTunnels() []*Tunnel {
	out := m.Tunnels()
	m.mu.RLock()
	defer m.mu.RUnlock()
	for slot := range m.deletedSlots {
		out = append(out, &Tunnel{Slot: slot, Status: "stopped"})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slot < out[j].Slot })
	return out
}

// nodeInUse 判断某节点是否已被别的隧道占用。
func (m *Manager) nodeInUse(host string, exceptSlot int) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for slot, t := range m.tunnels {
		t.mu.Lock()
		matches := slot != exceptSlot && t.Status != "stopped" && t.Node.HostName == host
		t.mu.Unlock()
		if matches {
			return true
		}
	}
	return false
}

func (m *Manager) ownsTunnel(t *Tunnel) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.tunnels[t.Slot] == t
}
