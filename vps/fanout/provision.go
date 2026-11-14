package main

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// ProvisionRequest 是"给我 N 个某地区的出口"这个意图。
type ProvisionRequest struct {
	Region     string // 国家码，空表示不限
	Count      int
	TemplateID int // 内置 Xray 入站模板；0 表示只开隧道不建入站
	// EveryRegion 表示每个有节点的国家都来 Count 个，此时 Region 被忽略。
	EveryRegion bool
	Policy      *SlotPolicy
}

// Provision 异步执行一次批量开出口，立刻返回作业句柄供界面轮询。
//
// 隧道并行拉起（每条都要等 openvpn 握手，串行会线性累加等待），
// 面板侧的入站创建则统一放到最后串行做一次，因为每次改路由都要重启 Xray。
func (m *Manager) Provision(req ProvisionRequest) (*Job, error) {
	if req.Count < 1 {
		return nil, fmt.Errorf("数量至少为 1")
	}
	picks, policy, err := m.planProvision(req)
	if err != nil {
		return nil, err
	}

	labels := make([]string, 0, len(picks)+1)
	for _, n := range picks {
		labels = append(labels, regionLabel(n)+" 出口")
	}
	if req.TemplateID > 0 {
		labels = append(labels, "创建节点链接")
	}

	title := ""
	switch {
	case req.EveryRegion:
		title = fmt.Sprintf("每个国家开 %d 个，共 %d 条出口", req.Count, len(picks))
	case req.Region != "":
		title = fmt.Sprintf("开 %d 个 %s 出口", len(picks), req.Region)
	default:
		title = fmt.Sprintf("开 %d 个任意地区出口", len(picks))
	}
	job := m.jobs.New(title, labels)

	go m.runProvision(job, picks, req.TemplateID, policy, req.EveryRegion)
	return job, nil
}

func (m *Manager) runProvision(job *Job, picks []Node, templateID int, policy SlotPolicy, every bool) {
	defer job.Finish()

	var wg sync.WaitGroup
	started := make([]*Tunnel, len(picks))

	for i, node := range picks {
		p := policy
		if every {
			p.Country = node.CountryCode
		}
		t, err := m.StartWithPolicy(node, p)
		if err != nil {
			job.Set(i, "failed", err.Error())
			continue
		}
		started[i] = t
		job.Set(i, "running", "正在连接 "+node.HostName)

		wg.Add(1)
		go func(i int, t *Tunnel) {
			defer wg.Done()
			m.waitUp(t)
			if t.Status == "up" {
				job.Set(i, "ok", t.ExitIP)
				return
			}
			job.Set(i, "failed", firstLine(t.Err))
		}(i, t)
	}
	wg.Wait()

	if templateID <= 0 {
		return
	}

	step := len(picks)
	var hosts []string
	for _, t := range started {
		if t != nil && t.Status == "up" {
			hosts = append(hosts, t.Node.HostName)
		}
	}
	if len(hosts) == 0 {
		job.Set(step, "failed", "没有连通的出口，跳过")
		return
	}

	job.Set(step, "running", fmt.Sprintf("为 %d 个出口建入站", len(hosts)))
	x, err := openPanel()
	if err != nil {
		job.Set(step, "failed", err.Error())
		return
	}
	ports, err := x.CloneToTunnels(templateID, hosts, m.Tunnels())
	invalidateInbounds()
	if err != nil {
		job.Set(step, "failed", firstLine(err.Error()))
		return
	}
	job.Set(step, "ok", fmt.Sprintf("已创建 %d 个入站", len(ports)))
}

// planProvision keeps missing candidates as waiting intent instead of silently
// selecting another country or relaxing allowed types/ISP/ASN.
func (m *Manager) planProvision(req ProvisionRequest) ([]Node, SlotPolicy, error) {
	p := defaultSlotPolicy(req.Region)
	if req.Policy != nil {
		p = *req.Policy
	}
	if req.Region != "" && p.Country != "" && !strings.EqualFold(req.Region, p.Country) {
		return nil, p, fmt.Errorf("地区和策略国家冲突")
	}
	if req.Region != "" {
		p.Country = req.Region
	}
	var err error
	p, err = normalizePolicy(p)
	if err != nil {
		return nil, p, err
	}
	m.mu.RLock()
	room := m.maxSlots - len(m.tunnels)
	m.mu.RUnlock()
	if room <= 0 {
		return nil, p, fmt.Errorf("Slot 已满；停止保留 Slot，删除才释放")
	}
	if req.Count < 1 || req.Count > room {
		return nil, p, fmt.Errorf("数量超出空闲 Slot 数量")
	}
	candidates := m.policyCandidates(p, nil, 0)
	if len(candidates) == 0 && residentialOnly() {
		if volunteers, total := m.volunteerPoolStats(); total > 0 && volunteers == 0 {
			return nil, p, fmt.Errorf("没有可用的志愿者家宽节点；可在设置里关闭「只使用志愿者家宽」，或等待来源刷新")
		}
	}
	if !req.EveryRegion {
		out := make([]Node, 0, req.Count)
		for _, n := range candidates {
			if len(out) < req.Count {
				out = append(out, n)
			}
		}
		for len(out) < req.Count {
			out = append(out, Node{CountryCode: p.Country, Country: p.Country})
		}
		return out, p, nil
	}
	if p.Country != "" {
		return nil, p, fmt.Errorf("每地区模式不能同时固定国家")
	}
	out, counts := []Node{}, map[string]int{}
	for _, n := range candidates {
		if n.CountryCode == "" || counts[n.CountryCode] >= req.Count {
			continue
		}
		out = append(out, n)
		counts[n.CountryCode]++
		if len(out) == room {
			break
		}
	}
	if len(out) == 0 {
		return nil, p, fmt.Errorf("没有可发现地区；可直接指定国家创建等待中的 Slot")
	}
	return out, p, nil
}

// waitUp 等一条隧道跑完 bringUp。bringUp 最多试 6 个候选节点，
// 每个节点等 tun0 最长 40 秒，所以这里给足余量。
func (m *Manager) waitUp(t *Tunnel) {
	const maxWait = 5 * time.Minute
	deadline := time.Now().Add(maxWait)
	for time.Now().Before(deadline) {
		if t.Status == "up" || t.Status == "degraded" || t.Status == "failed" || t.Status == "waiting_for_nodes" || t.Status == "blocked" || t.Status == "stopped" {
			return
		}
		time.Sleep(time.Second)
	}
}

// nodePoolLocked 返回可以拿来挑的节点。开了「只使用志愿者家宽」就把
// VPN Gate 自营机房的节点剔掉；这里判断的是来源标签 SourceResidential，
// 出口画像是否住宅由 Slot 策略另行判断。调用方必须已持 m.mu。
//
// 这条过滤覆盖所有自动选点：控制台补齐、批量开出口、初始出口、健康重连
// 和手动换节点。已经跑起来的隧道不会因为打开开关而被换掉；重连时当前节点
// 仍由 candidatesFor 单独放在首位用于恢复。
func (m *Manager) nodePoolLocked() []Node {
	if !residentialOnly() {
		return m.nodes
	}
	out := make([]Node, 0, len(m.nodes))
	for _, n := range m.nodes {
		if n.SourceResidential {
			out = append(out, n)
		}
	}
	return out
}

// volunteerPoolStats reports how many candidates survive the source filter.
// total includes every pool entry, so callers can tell "pool empty" from
// "pool non-empty but no volunteer nodes".
func (m *Manager) volunteerPoolStats() (volunteers, total int) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, n := range m.nodes {
		if n.SourceResidential {
			volunteers++
		}
	}
	return volunteers, len(m.nodes)
}

// RegionStat 是某个地区的可用节点概况，用于新建向导里的地区选择。
type RegionStat struct {
	Code      string  `json:"code"`
	Name      string  `json:"name"`
	Available int     `json:"available"`
	BestPing  int     `json:"best_ping"`
	BestSpeed float64 `json:"best_speed_mbps"`
}

// Regions 汇总各地区还剩多少空闲节点，按可用数量降序。
func (m *Manager) Regions() []RegionStat {
	m.mu.RLock()
	defer m.mu.RUnlock()

	used := map[string]bool{}
	for _, t := range m.tunnels {
		used[t.Node.HostName] = true
	}

	byCode := map[string]*RegionStat{}
	for _, n := range m.nodePoolLocked() {
		if used[n.HostName] || n.CountryCode == "" {
			continue
		}
		s := byCode[n.CountryCode]
		if s == nil {
			s = &RegionStat{Code: n.CountryCode, Name: countryLabel(n.CountryCode, n.Country), BestPing: n.Ping}
			byCode[n.CountryCode] = s
		}
		s.Available++
		if n.SpeedMbps > s.BestSpeed {
			s.BestSpeed = n.SpeedMbps
		}
		if n.Ping > 0 && (s.BestPing == 0 || n.Ping < s.BestPing) {
			s.BestPing = n.Ping
		}
	}

	out := make([]RegionStat, 0, len(byCode))
	for _, s := range byCode {
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Available != out[j].Available {
			return out[i].Available > out[j].Available
		}
		return out[i].Code < out[j].Code
	})
	return out
}

// regionLabel 给出口起一个人能读的名字。
func regionLabel(n Node) string {
	if place := nodeLabel(n); place != "" {
		return place
	}
	return n.HostName
}

// firstLine 截取错误的第一行，界面里放得下。
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i > 0 {
		return s[:i]
	}
	return s
}
