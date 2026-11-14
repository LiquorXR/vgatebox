package main

import (
	"os"
	"strings"
	"testing"
)

// Production defaults the volunteer-only switch to on. Most fixtures model an
// unfiltered pool, so tests opt into the filter explicitly with
// withResidentialOnly instead of depending on the ambient default.
func TestMain(m *testing.M) {
	webSettingsMu.Lock()
	off := false
	webSettingsCur.ResidentialOnly = &off
	webSettingsMu.Unlock()
	os.Exit(m.Run())
}

// withResidentialOnly 临时改"只用家宽"开关，不落盘，测完自动还原。
func withResidentialOnly(t *testing.T, v *bool) {
	t.Helper()
	webSettingsMu.Lock()
	prev := webSettingsCur.ResidentialOnly
	webSettingsCur.ResidentialOnly = v
	webSettingsMu.Unlock()
	t.Cleanup(func() {
		webSettingsMu.Lock()
		webSettingsCur.ResidentialOnly = prev
		webSettingsMu.Unlock()
	})
}

func boolPtr(v bool) *bool { return &v }

func TestIsResidential(t *testing.T) {
	cases := []struct {
		host, ip string
		want     bool
	}{
		// vpngate 自营：hostname 前缀
		{"public-vpn-123", "1.2.3.4", false},
		{"PUBLIC-VPN-9", "1.2.3.4", false},
		// vpngate 自营：学术网络机房那一段
		{"vpn829571948", "219.100.37.117", false},
		{"vpn123", "219.100.37.0", false},
		{"vpn123", "219.100.37.255", false},
		// 志愿者家宽
		{"vpn829571948", "60.101.169.14", true},
		{"vpn123", "219.100.38.1", true},
		{"vpn123", "219.100.36.255", true},
		// IP 读不出来时不敢断言，放过
		{"vpn123", "", true},
		{"vpn123", "不是IP", true},
	}
	for _, c := range cases {
		if got := isResidential(c.host, c.ip); got != c.want {
			t.Fatalf("isResidential(%q,%q)=%v，想要 %v", c.host, c.ip, got, c.want)
		}
	}
}

var mixed = []Node{
	{HostName: "public-vpn-1", CountryCode: "JP", Country: "Japan", SpeedMbps: 900, SourceResidential: false},
	{HostName: "jp-home", CountryCode: "JP", Country: "Japan", SpeedMbps: 300, SourceResidential: true},
	{HostName: "kr-dc", CountryCode: "KR", Country: "Korea", SpeedMbps: 500, SourceResidential: false},
}

// 地区统计与实际可分配一致：开着来源过滤时，机房节点不再计入。
func TestRegionsIncludesUnverifiedCandidates(t *testing.T) {
	withResidentialOnly(t, boolPtr(true))
	m := mgrWith(mixed)
	seen := map[string]int{}
	for _, r := range m.Regions() {
		seen[r.Code] = r.Available
	}
	if seen["JP"] != 1 {
		t.Fatalf("JP 只剩一个志愿者候选，实际 %d", seen["JP"])
	}
	if _, ok := seen["KR"]; ok {
		t.Fatal("只有机房节点的地区不应计入可分配数量")
	}
}

// 没配过这个开关时默认是开的：fanout 的意义就是把家宽扇成出口。
func TestResidentialOnlyDefaultsOn(t *testing.T) {
	withResidentialOnly(t, nil)
	if !residentialOnly() {
		t.Fatal("没配过时应默认只用家宽")
	}
}

// 自动选点必须尊重来源开关：控制台补齐、批量开出口、重连都走 policyCandidates。
func TestPolicyCandidatesSkipsHostingWhenResidentialOnly(t *testing.T) {
	withResidentialOnly(t, boolPtr(true))
	m := mgrWith(mixed)
	got := m.policyCandidates(defaultSlotPolicy(""), nil, 0)
	if len(got) != 1 || got[0].HostName != "jp-home" {
		t.Fatalf("volunteer-only pool leaked hosting nodes: %+v", got)
	}
}

func TestPolicyCandidatesIncludesHostingWhenSourceFilterOff(t *testing.T) {
	withResidentialOnly(t, boolPtr(false))
	m := mgrWith(mixed)
	if got := m.policyCandidates(defaultSlotPolicy(""), nil, 0); len(got) != 3 {
		t.Fatalf("source filter off should keep every node: %+v", got)
	}
}

func TestCandidatesForKeepsHostingCurrentNodeButFiltersAlternatives(t *testing.T) {
	withResidentialOnly(t, boolPtr(true))
	m := mgrWith(mixed)
	policy := defaultSlotPolicy("")
	tn := &Tunnel{Slot: 1, Node: mixed[2], Status: "up", Policy: &policy} // kr-dc hosting source
	m.tunnels[1] = tn
	got := m.candidatesFor(tn)
	if len(got) == 0 || got[0].HostName != "kr-dc" {
		t.Fatalf("current node must stay first for recovery: %+v", got)
	}
	for _, n := range got[1:] {
		if !n.SourceResidential {
			t.Fatalf("alternative hosting node leaked into a volunteer-only reconnect: %+v", got)
		}
	}
}

func TestDefaultSlotPolicyKeepsSourceFilterDecoupled(t *testing.T) {
	withResidentialOnly(t, boolPtr(true))
	if !containsFold(defaultSlotPolicy("").AllowedIPTypes, "unknown") {
		t.Fatal("source switch must not force a strict-only IP policy")
	}
}

func TestPlanProvisionReportsEmptyVolunteerPool(t *testing.T) {
	withResidentialOnly(t, boolPtr(true))
	m := mgrWith([]Node{{HostName: "public-vpn-1", CountryCode: "JP", SourceResidential: false}})
	if _, _, err := m.planProvision(ProvisionRequest{Count: 1}); err == nil || !strings.Contains(err.Error(), "志愿者") {
		t.Fatalf("empty volunteer pool was not reported: %v", err)
	}
}

// ---- 换节点：不能换回刚才那个 ----

var pool = []Node{
	{HostName: "jp1", CountryCode: "JP", SpeedMbps: 900, SourceResidential: true},
	{HostName: "jp2", CountryCode: "JP", SpeedMbps: 800, SourceResidential: true},
	{HostName: "jp3", CountryCode: "JP", SpeedMbps: 700, SourceResidential: true},
}

// 核心回归：换过的节点会记进历史，再挑时被跳过。
// 改之前 jp1→jp2 之后 jp1 就空出来了，而它速度最快又排在最前面，
// 于是第二次换节点又换回 jp1，界面上看着像"没换"。
func TestSwapAvoidsPreviousNode(t *testing.T) {
	withResidentialOnly(t, boolPtr(true))
	m := mgrWith(pool)
	tn := &Tunnel{Slot: 1, Node: pool[0], Status: "up"}
	m.tunnels[1] = tn

	seen := []string{tn.Node.HostName}
	for i := 0; i < 2; i++ {
		node, err := m.pickSwapTarget(tn)
		if err != nil {
			t.Fatalf("第 %d 次换节点挑不到: %v", i+1, err)
		}
		tn.Node = node
		for _, h := range seen {
			if h == tn.Node.HostName {
				t.Fatalf("第 %d 次换节点又换回了用过的 %s", i+1, h)
			}
		}
		seen = append(seen, tn.Node.HostName)
	}
	if len(seen) != 3 {
		t.Fatalf("三个节点应各用一次，实际 %v", seen)
	}
}

// 真机踩到的那条：挑中的节点连不上时会被候选列表换成别人，
// 如果只记"换下来的那个"，下次点换节点又会从这个连不上的开始试。
func TestSwapRemembersFailedPick(t *testing.T) {
	withResidentialOnly(t, boolPtr(true))
	m := mgrWith(pool)
	tn := &Tunnel{Slot: 1, Node: pool[0], Status: "up"} // 当前 jp1
	m.tunnels[1] = tn

	// 第一次换节点：挑中 jp2
	first, err := m.pickSwapTarget(tn)
	if err != nil {
		t.Fatal(err)
	}
	if first.HostName != "jp2" {
		t.Fatalf("按速度该挑到 jp2，实际 %s", first.HostName)
	}
	// jp2 连不上，候选机制把隧道落到了 jp3
	tn.Node = pool[2]

	// 第二次换节点：不能再挑 jp2，它刚才就连不上
	second, err := m.pickSwapTarget(tn)
	if err == nil && second.HostName == "jp2" {
		t.Fatal("又挑中了刚才连不上的 jp2，等于白等一轮握手")
	}
	// 三个节点都试过了，这轮该清历史重来，挑到的只要不是当前的 jp3 就对
	if err != nil {
		t.Fatalf("历史清空后应该还能挑: %v", err)
	}
	if second.HostName == "jp3" {
		t.Fatal("不能换成当前这个")
	}
}

// 一轮换完之后清历史，让用户还能接着换，而不是收到"没得换了"。
func TestSwapHistoryResetAfterFullRound(t *testing.T) {
	withResidentialOnly(t, boolPtr(true))
	m := mgrWith(pool)
	tn := &Tunnel{Slot: 1, Node: pool[2], Status: "up"}
	m.tunnels[1] = tn
	tn.rememberSwap("jp1")
	tn.rememberSwap("jp2")

	// 三个节点都用过：现行 pickSwapTarget 应清历史重来，挑到的只要不是当前的 jp3 就对。
	got, err := m.pickSwapTarget(tn)
	if err != nil {
		t.Fatalf("历史清空后应该还能挑: %v", err)
	}
	if got.HostName == "jp3" {
		t.Fatal("清历史也不能换成当前这个")
	}
}

// 历史有上限，不能无限攒着把自动重连的候选面排干。
func TestSwapHistoryCapped(t *testing.T) {
	tn := &Tunnel{Slot: 1, Node: Node{HostName: "cur"}}
	for i := 0; i < swapHistoryMax+5; i++ {
		tn.rememberSwap(string(rune('a'+i%26)) + string(rune('0'+i/26)))
	}
	if len(tn.swapped) != swapHistoryMax {
		t.Fatalf("历史应封顶在 %d，实际 %d", swapHistoryMax, len(tn.swapped))
	}
	// 同一个节点重复记也不该占多个位置
	n := len(tn.swapped)
	tn.rememberSwap(tn.swapped[0])
	if len(tn.swapped) != n {
		t.Fatal("重复的 hostname 不该重复入历史")
	}
}

// 自动重连仍以当前节点打头（目标是恢复），但备选要避开用户手动换掉的。
func TestCandidatesForKeepsCurrentFirstAndAvoidsSwapped(t *testing.T) {
	withResidentialOnly(t, boolPtr(true))
	m := mgrWith(pool)
	tn := &Tunnel{Slot: 1, Node: pool[1], Status: "up"} // 当前 jp2
	m.tunnels[1] = tn
	tn.rememberSwap("jp1") // jp1 是用户换掉的

	got := m.candidatesFor(tn)
	if len(got) == 0 || got[0].HostName != "jp2" {
		t.Fatalf("首位必须是当前节点，实际 %+v", got)
	}
	for _, n := range got[1:] {
		if n.HostName == "jp1" {
			t.Fatal("备选里不该有用户换掉的 jp1")
		}
	}
}

// ---- 每个国家各开几个 ----

var manyRegions = []Node{
	{HostName: "jp1", CountryCode: "JP", Country: "Japan", SpeedMbps: 900, SourceResidential: true},
	{HostName: "jp2", CountryCode: "JP", Country: "Japan", SpeedMbps: 800, SourceResidential: true},
	{HostName: "jp3", CountryCode: "JP", Country: "Japan", SpeedMbps: 700, SourceResidential: true},
	{HostName: "kr1", CountryCode: "KR", Country: "Korea", SpeedMbps: 600, SourceResidential: true},
	{HostName: "kr2", CountryCode: "KR", Country: "Korea", SpeedMbps: 500, SourceResidential: true},
	{HostName: "us1", CountryCode: "US", Country: "United States", SpeedMbps: 400, SourceResidential: true},
	{HostName: "dc1", CountryCode: "DE", Country: "Germany", SpeedMbps: 999, SourceResidential: false},
	{HostName: "none", CountryCode: "", Country: "", SpeedMbps: 300, SourceResidential: true},
}

func TestPlanProvisionEveryRegion(t *testing.T) {
	withResidentialOnly(t, boolPtr(true))
	m := mgrWith(manyRegions)
	picks, _, err := m.planProvision(ProvisionRequest{Count: 1, EveryRegion: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(picks) != 3 {
		t.Fatalf("JP/KR/US 各一个，应当 3 条，实际 %d: %+v", len(picks), picks)
	}
	seen := map[string]string{}
	for _, n := range picks {
		if prev, dup := seen[n.CountryCode]; dup {
			t.Fatalf("%s 出现两次: %s / %s", n.CountryCode, prev, n.HostName)
		}
		seen[n.CountryCode] = n.HostName
	}
	if seen["JP"] != "jp1" || seen["KR"] != "kr1" {
		t.Fatalf("每国该挑最快的，实际 %+v", seen)
	}
	if _, ok := seen["DE"]; ok {
		t.Fatal("开着家宽过滤时不该带上机房节点")
	}
	if _, ok := seen[""]; ok {
		t.Fatal("没有国家码的节点不该单独算一个国家")
	}

	// 每国多条：JP 3 取 2，KR 2 取 2，US 只有 1 个给 1。
	picks, _, err = m.planProvision(ProvisionRequest{Count: 2, EveryRegion: true})
	if err != nil {
		t.Fatal(err)
	}
	count := map[string]int{}
	for _, n := range picks {
		count[n.CountryCode]++
	}
	if count["JP"] != 2 || count["KR"] != 2 || count["US"] != 1 {
		t.Fatalf("每国条数不对: %+v", count)
	}

	// 槽位不够时按 room 截断；槽位满时报错含槽位。
	m2 := NewManager(2, t_tmpdir)
	m2.nodes = manyRegions
	picks, _, err = m2.planProvision(ProvisionRequest{Count: 1, EveryRegion: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(picks) != 2 {
		t.Fatalf("上限 2 个槽位，应当只挑 2 条，实际 %d", len(picks))
	}
	m3 := NewManager(1, t_tmpdir)
	m3.nodes = manyRegions
	m3.tunnels[1] = &Tunnel{Slot: 1, Node: manyRegions[0], Status: "up"}
	if _, _, err := m3.planProvision(ProvisionRequest{Count: 1, EveryRegion: true}); err == nil {
		t.Fatal("槽位满了应当报错")
	}

	// 已经在用的节点不该被再挑。
	m4 := mgrWith(manyRegions, "jp1", "kr1")
	picks, _, err = m4.planProvision(ProvisionRequest{Count: 1, EveryRegion: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range picks {
		if n.HostName == "jp1" || n.HostName == "kr1" {
			t.Fatalf("%s 已经在用了", n.HostName)
		}
	}
}
