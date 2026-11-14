package main

// console_sstp.go：SSTP 类型、设置校验与落盘。
import (
	"context"
	"encoding/json"
	"fanout/internal/paths"
	"fmt"
	"os"
	"sync"
	"time"
)

type SstpSettings struct {
	Parallelism     int `json:"parallelism"`
	IntervalMinutes int `json:"interval_minutes"`
	ValidMinutes    int `json:"valid_minutes"`
	TimeoutSeconds  int `json:"timeout_seconds"`
}

type sstpState struct {
	Settings    SstpSettings           `json:"settings"`
	Results     map[string]CheckResult `json:"results"`
	LastStarted time.Time              `json:"last_started"`
}

type SstpManager struct {
	mu           sync.Mutex
	c            *Console
	state        sstpState
	job          *CheckRun
	lastAttempts map[string]CheckResult // Session attempts, separate from exportable observations.
	probe        func(context.Context, Node, SstpSettings, func(string)) CheckResult
	dependencies func() error
}

func validateSstpSettings(v SstpSettings) error {
	if err := validateCheckParallelism(v.Parallelism); err != nil {
		return err
	}
	if v.IntervalMinutes < 0 || v.IntervalMinutes > 525600 || v.ValidMinutes < 1 || v.ValidMinutes > 10080 || v.TimeoutSeconds < 10 || v.TimeoutSeconds > 3600 {
		return fmt.Errorf("并发须为正整数；周期 0 表示关闭，有效期 1–10080 分钟，超时 10–3600 秒")
	}
	return nil
}

func (c *Console) loadSstpManager() error {
	s := &SstpManager{c: c, state: sstpState{Settings: SstpSettings{Parallelism: c.checkParallelism, ValidMinutes: 60, TimeoutSeconds: 90}, Results: map[string]CheckResult{}}}
	blob, err := os.ReadFile(paths.SSTP(c.m.workDir))
	if err == nil {
		if json.Unmarshal(blob, &s.state) != nil || validateSstpSettings(s.state.Settings) != nil {
			return fmt.Errorf("SSTP 设置或结果文件损坏，保留原文件")
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if s.state.Results == nil {
		s.state.Results = map[string]CheckResult{}
	}
	s.probe = s.probeNode
	s.lastAttempts = map[string]CheckResult{}
	s.dependencies = sstpDependencies
	c.sstp = s
	return nil
}

func (s *SstpManager) persist() error {
	blob, err := json.Marshal(s.state)
	if err != nil {
		return err
	}
	if err = atomicConfigWrite(paths.SSTP(s.c.m.workDir), blob); err != nil {
		return fmt.Errorf("SSTP 结果或设置保存失败")
	}
	return nil
}

func sstpProfile(n Node) string {
	blob, _ := json.Marshal(struct {
		Host, IP   string
		Candidates []SstpEndpoint
	}{n.HostName, n.IP, n.SSTPCandidates})
	return profileDigest(string(blob))
}

func freshSstpResult(n Node, r CheckResult, now time.Time) CheckResult {
	r.Protocol = "sstp"
	r.Stale = r.CheckedAt.After(now) || !r.ExpiresAt.After(r.CheckedAt) || r.ProfileSHA != sstpProfile(n) || r.CheckedAt.IsZero() || !r.ExpiresAt.After(now)
	if r.Status == "passed" && (!sstpPublicIPv4(r.ActualIP) || r.ObservedBy != "vps" || r.Port < 1 || r.Port > 65535) {
		r.Stale = true
	}
	matched := false
	for _, endpoint := range n.SSTPCandidates {
		if officialSstpEndpoint(n, endpoint) && endpoint.Port == r.Port {
			matched = true
		}
	}
	if r.Status == "passed" && !matched {
		r.Stale = true
	}
	r.Country, r.IPType = "", "unknown"
	if r.Egress != nil {
		image := *r.Egress
		expiry, err := time.Parse(time.RFC3339Nano, image.ExpiresAt)
		if image.Protocol == "sstp" && image.IP == r.ActualIP && image.IntelligenceStatus == "passed" && err == nil && expiry.After(now) && image.Provider != "" && image.QueriedAt != "" && image.RuleVersion != "" {
			r.Country, r.IPType = verifiedProfileCountry(image), image.IPType
		}
		if image.Protocol == "sstp" && image.IP == r.ActualIP {
			r.Country = verifiedProfileCountry(image)
		}
		image.Raw = nil
		r.Egress = &image
	}
	r.CFStatus = "not_tested"
	return r
}
