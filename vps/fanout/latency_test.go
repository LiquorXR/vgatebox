package main

import (
	"math"
	"testing"
	"time"
)

func detailedSample(ip string, total float64) exitIPProbeDetailed {
	return exitIPProbeDetailed{ip: ip, appMS: total * 0.3, startMS: total * 0.6, totalMS: total}
}

func TestProbeSocksMedianTakesUpperMedianAndMaxAcrossEchoes(t *testing.T) {
	calls := map[string]int{}
	ip, latency, detail, err := probeSocksMedianPair(func(url string) ([]exitIPProbeDetailed, error) {
		calls[url]++
		if url == exitIPEchoURLs[0] {
			return []exitIPProbeDetailed{
				detailedSample("73.1.1.1", 100),
				detailedSample("73.1.1.1", 300),
				detailedSample("73.1.1.1", 200),
			}, nil
		}
		return []exitIPProbeDetailed{
			detailedSample("73.1.1.1", 50),
			detailedSample("73.1.1.1", 60),
		}, nil
	})
	// url0 上中位数 200，url1 上中位数 60（2 个取较大者），最终取较大者 200。
	if err != nil || ip != "73.1.1.1" || latency != 200 || detail == nil || detail.Samples != 3 {
		t.Fatalf("median wrong: %s %v %+v %v", ip, latency, detail, err)
	}
	if math.Abs(detail.TLSMS-60) > 1e-6 || math.Abs(detail.TTFBMS-60) > 1e-6 {
		t.Fatalf("breakdown wrong: %+v", detail)
	}
	if len(calls) != 2 {
		t.Fatalf("both echoes did not run: %v", calls)
	}
}

func TestProbeSocksMedianRejectsDriftMismatchAndEmpties(t *testing.T) {
	if _, _, _, err := probeSocksMedianPair(func(string) ([]exitIPProbeDetailed, error) {
		return []exitIPProbeDetailed{detailedSample("73.1.1.1", 100), detailedSample("73.1.1.2", 100)}, nil
	}); err == nil {
		t.Fatal("in-sample drift accepted")
	}
	first := true
	if _, _, _, err := probeSocksMedianPair(func(string) ([]exitIPProbeDetailed, error) {
		if first {
			first = false
			return []exitIPProbeDetailed{detailedSample("73.1.1.1", 100)}, nil
		}
		return []exitIPProbeDetailed{detailedSample("73.1.1.2", 100)}, nil
	}); err == nil {
		t.Fatal("cross-echo mismatch accepted")
	}
	if _, _, _, err := probeSocksMedianPair(func(string) ([]exitIPProbeDetailed, error) {
		return nil, nil
	}); err == nil {
		t.Fatal("empty samples accepted")
	}
}

func TestParseSocksProbeDetailed(t *testing.T) {
	d, err := parseSocksProbeDetailed([]byte("73.1.1.1\n0.100 0.200 0.350"))
	if err != nil || d.ip != "73.1.1.1" || d.totalMS != 350 {
		t.Fatalf("detailed parse wrong: %+v %v", d, err)
	}
	if math.Abs(d.appMS-100) > 1e-6 || math.Abs(d.startMS-d.appMS-100) > 1e-6 {
		t.Fatalf("detailed segments wrong: %+v", d)
	}
	for _, body := range []string{
		"73.1.1.1\n0.350",
		"73.1.1.1\n0.300 0.200 0.350",
		"73.1.1.1\nNaN 0.200 0.350",
		"not-an-ip\n0.100 0.200 0.350",
		"73.1.1.1",
	} {
		if _, err := parseSocksProbeDetailed([]byte(body)); err == nil {
			t.Fatalf("malformed detailed sample accepted: %q", body)
		}
	}
}

func TestEWMASmoothsAndInitializes(t *testing.T) {
	tun := &Tunnel{Slot: 1}
	tun.absorbLatency(100)
	if tun.LatencyMS != 100 {
		t.Fatalf("first sample did not initialize: %v", tun.LatencyMS)
	}
	tun.absorbLatency(200)
	if math.Abs(tun.LatencyMS-140) > 1e-6 || tun.latencyInstantMS != 200 {
		t.Fatalf("ewma wrong: smooth=%v instant=%v", tun.LatencyMS, tun.latencyInstantMS)
	}
}

func TestObservationPerfLatencyExpiresBeforeRank(t *testing.T) {
	m := NewManager(4, t.TempDir())
	m.nodes = []Node{{HostName: "a", Config: "a"}, {HostName: "b", Config: "b"}}
	now := time.Now()
	m.recordConnectivity(CheckResult{Host: "a", ProfileSHA: profileDigest("a"), Status: "passed", LatencyMS: 10, CheckedAt: now.Add(-11 * time.Minute)})
	m.recordConnectivity(CheckResult{Host: "b", ProfileSHA: profileDigest("b"), Status: "passed", LatencyMS: 900, CheckedAt: now})
	// 档位都是 0（30 分钟内），但 a 的延迟已过 10 分钟性能窗口，按未知排在 b 之后。
	picks := m.policyCandidates(defaultSlotPolicyForMode("", false), nil, 0)
	if len(picks) != 2 || picks[0].HostName != "b" || picks[1].HostName != "a" {
		t.Fatalf("stale latency still ordered first: %v", picks)
	}
}

func TestFailRateOrdersFlappingBelowStable(t *testing.T) {
	m := NewManager(4, t.TempDir())
	m.nodes = []Node{{HostName: "flappy", Config: "a"}, {HostName: "stable", Config: "b"}}
	now := time.Now()
	for i := 0; i < 4; i++ {
		m.recordConnectivity(CheckResult{Host: "flappy", ProfileSHA: profileDigest("a"), Status: "failed", CheckedAt: now.Add(time.Duration(i) * time.Second)})
	}
	m.recordConnectivity(CheckResult{Host: "flappy", ProfileSHA: profileDigest("a"), Status: "passed", LatencyMS: 10, CheckedAt: now.Add(5 * time.Second)})
	m.recordConnectivity(CheckResult{Host: "stable", ProfileSHA: profileDigest("b"), Status: "passed", LatencyMS: 10, CheckedAt: now.Add(6 * time.Second)})
	// 同档同延迟，失败率 0.8 的 flappy 排在 stable 之后。
	picks := m.policyCandidates(defaultSlotPolicyForMode("", false), nil, 0)
	if len(picks) != 2 || picks[0].HostName != "stable" || picks[1].HostName != "flappy" {
		t.Fatalf("flapping host not penalized: %v", picks)
	}
}
