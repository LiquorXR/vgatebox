package main

// console_sstp_run.go：SSTP 检测运行与调度。
import (
	"context"
	"fmt"
	"time"
)

// Reuse the same dispatcher as OpenVPN, but never reuse its connections/evidence.
func (s *SstpManager) start(in CheckRequest) (*CheckRun, error) {
	nodes, _ := s.c.m.Nodes()
	byHost := map[string]Node{}
	for _, n := range nodes {
		if len(n.SSTPCandidates) > 0 {
			byHost[n.HostName] = n
			if in.All {
				in.Hosts = append(in.Hosts, n.HostName)
			}
		}
	}
	items := []CheckResult{}
	seen := map[string]bool{}
	for _, host := range in.Hosts {
		n, ok := byHost[host]
		if !ok {
			return nil, fmt.Errorf("候选不在当前 SSTP 清单中")
		}
		if !seen[host] {
			seen[host] = true
			items = append(items, CheckResult{Host: host, Status: "queued", ProfileSHA: sstpProfile(n), Priority: in.Priority, ObservedBy: "vps", CFStatus: "not_tested"})
		}
	}
	if len(items) == 0 || len(in.Slots) > 0 {
		return nil, fmt.Errorf("请选择 SSTP 候选，不使用出口编号")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.c.ctx.Err() != nil {
		return nil, fmt.Errorf("管理服务正在停止")
	}
	if s.dependencies != nil {
		if err := s.dependencies(); err != nil {
			return nil, err
		}
	}
	if s.job != nil {
		j := s.job
		j.mu.Lock()
		if j.Status == "running" {
			if !in.Priority || j.ctx.Err() != nil {
				j.mu.Unlock()
				return nil, fmt.Errorf("已有 SSTP 检测任务，请等待或取消")
			}
			for _, item := range items {
				found := false
				for i := range j.Items {
					old := &j.Items[i]
					if old.Host == item.Host && (old.Status == "queued" || old.Status == "running") {
						old.Priority = true
						found = true
						break
					}
				}
				if !found {
					j.Items = append(j.Items, item)
					j.Total++
				}
			}
			for host, n := range byHost {
				j.nodes[host] = n
			}
			j.mu.Unlock()
			select {
			case j.wake <- struct{}{}:
			default:
			}
			return j, nil
		}
		// Keep terminal attempts when a new job replaces the previous job. A
		// deferred/cancelled attempt must not turn back into "not tested", or
		// overwrite an earlier measured observation used by private exports.
		for _, result := range j.Items {
			s.lastAttempts[result.Host] = result
		}
		j.mu.Unlock()
	}
	settings := s.state.Settings
	ctx, cancel := context.WithCancel(s.c.ctx)
	j := &CheckRun{ID: randomHex(8), Kind: "sstp", Status: "running", Items: items, Total: len(items), Parallelism: settings.Parallelism, ctx: ctx, cancel: cancel, finished: make(chan struct{}), wake: make(chan struct{}, 1), nodes: byHost}
	before := s.state.LastStarted
	s.state.LastStarted = time.Now().UTC()
	if err := s.persist(); err != nil {
		s.state.LastStarted = before
		cancel()
		return nil, err
	}
	s.job = j
	go dispatchCheckRun(ctx, j, &s.mu, func(index, slot int, item CheckResult, n Node) { s.runItem(ctx, j, index, item, n, settings) })
	return j, nil
}

func (s *SstpManager) runItem(ctx context.Context, j *CheckRun, index int, item CheckResult, n Node, settings SstpSettings) {
	progress := func(phase string) { j.mu.Lock(); j.Items[index].Phase = phase; j.mu.Unlock() }
	r := s.probe(ctx, n, settings, progress)
	r.Host = n.HostName
	r.ProfileSHA = sstpProfile(n)
	r.StartedAt = item.StartedAt
	r.CheckedAt = time.Now().UTC()
	r.ExpiresAt = r.CheckedAt.Add(time.Duration(settings.ValidMinutes) * time.Minute)
	r.Priority = item.Priority
	r.Protocol = "sstp"
	r.ObservedBy = "vps"
	r.CFStatus = "not_tested"
	if ctx.Err() != nil {
		r.Status, r.Code = "cancelled", "cancelled"
	}
	if r.Status == "passed" || r.Status == "failed" {
		s.mu.Lock()
		before, exists := s.state.Results[n.HostName]
		s.state.Results[n.HostName] = r
		if err := s.persist(); err != nil {
			if exists {
				s.state.Results[n.HostName] = before
			} else {
				delete(s.state.Results, n.HostName)
			}
			r.Status, r.Code = "failed", "result_save_failed"
		}
		s.mu.Unlock()
	}
	s.mu.Lock()
	s.lastAttempts[n.HostName] = r
	s.mu.Unlock()
	j.mu.Lock()
	j.Items[index] = r
	j.Done++
	j.mu.Unlock()
}

func (s *SstpManager) shutdown() {
	s.mu.Lock()
	j := s.job
	if j != nil {
		j.cancel()
	}
	s.mu.Unlock()
	if j != nil {
		<-j.finished
	}
}

func (s *SstpManager) watch() {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.c.ctx.Done():
			return
		case now := <-ticker.C:
			s.runScheduled(now)
		}
	}
}

func (s *SstpManager) runScheduled(now time.Time) {
	s.mu.Lock()
	due := s.state.Settings.IntervalMinutes > 0 && now.Sub(s.state.LastStarted) >= time.Duration(s.state.Settings.IntervalMinutes)*time.Minute
	if s.job != nil {
		s.job.mu.Lock()
		due = due && s.job.Status != "running"
		s.job.mu.Unlock()
	}
	s.mu.Unlock()
	if due {
		_, _ = s.start(CheckRequest{All: true})
	}
}
