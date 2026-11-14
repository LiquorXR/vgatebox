package main

import "testing"

func mgrWith(nodes []Node, running ...string) *Manager {
	m := NewManager(20, t_tmpdir)
	m.nodes = nodes
	for i, h := range running {
		m.tunnels[i+1] = &Tunnel{Slot: i + 1, Node: Node{HostName: h}, Status: "up"}
	}
	return m
}

const t_tmpdir = "/tmp"

var sample = []Node{
	{HostName: "jp1", CountryCode: "JP", Country: "Japan", SpeedMbps: 300, Ping: 10, SourceResidential: true},
	{HostName: "jp2", CountryCode: "JP", Country: "Japan", SpeedMbps: 200, Ping: 20, SourceResidential: true},
	{HostName: "kr1", CountryCode: "KR", Country: "Korea", SpeedMbps: 150, Ping: 30, SourceResidential: true},
}

func TestRegionsExcludesRunning(t *testing.T) {
	m := mgrWith(sample, "jp1")
	for _, r := range m.Regions() {
		if r.Code == "JP" && r.Available != 1 {
			t.Fatalf("JP 应剩 1 个空闲，实际 %d", r.Available)
		}
		if r.Code == "KR" && r.BestSpeed != 150 {
			t.Fatalf("KR 最高速应为 150，实际 %v", r.BestSpeed)
		}
	}
}

func TestJobLifecycle(t *testing.T) {
	var s JobStore
	j := s.New("测试", []string{"a", "b"})
	if v := j.View(); v.Total != 2 || v.Done != 0 || v.Status != "running" {
		t.Fatalf("初始状态不对: %+v", v)
	}

	j.Set(0, "ok", "1.2.3.4")
	j.Set(1, "failed", "连不上")
	j.Finish()

	v := j.View()
	if v.Status != "failed" || v.Done != 2 {
		t.Fatalf("有失败步骤时整体应为 failed: %+v", v)
	}
	if v.Steps[0].Detail != "1.2.3.4" {
		t.Fatalf("步骤详情丢失: %+v", v.Steps[0])
	}

	s.Dismiss(j.ID())
	if len(s.Views()) != 0 {
		t.Fatal("Dismiss 后不应还留着")
	}
}

func TestPlanProvisionSkipsRunningAndValidates(t *testing.T) {
	withResidentialOnly(t, boolPtr(false))
	m := mgrWith(sample, "jp1")
	picks, _, err := m.planProvision(ProvisionRequest{Region: "JP", Count: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(picks) != 1 || picks[0].HostName == "jp1" {
		t.Fatalf("应跳过已占用的 jp1，实际 %+v", picks)
	}
	if _, _, err := m.planProvision(ProvisionRequest{Region: "JP", Count: 1, Policy: &SlotPolicy{Country: "KR"}}); err == nil {
		t.Fatal("地区和策略国家冲突应当报错")
	}
	if _, _, err := m.planProvision(ProvisionRequest{Count: 99}); err == nil {
		t.Fatal("数量超出空闲 Slot 应当报错")
	}
	if _, _, err := m.planProvision(ProvisionRequest{Region: "JP", Count: 1, EveryRegion: true}); err == nil {
		t.Fatal("每地区模式不能同时固定国家")
	}
}

func TestFirstLine(t *testing.T) {
	if got := firstLine("第一行\n第二行"); got != "第一行" {
		t.Fatalf("firstLine = %q", got)
	}
	if got := firstLine("只有一行"); got != "只有一行" {
		t.Fatalf("firstLine = %q", got)
	}
}
