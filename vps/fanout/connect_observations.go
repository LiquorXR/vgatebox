package main

import "time"

// Observations only affect selection order. Every new connection still passes
// the full isolation, policy and independent HTTPS/SOCKS checks.
func (m *Manager) recordConnectivity(r CheckResult) {
	if r.Host == "" || (r.Status != "passed" && r.Status != "failed") {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.observations == nil {
		m.observations = map[string]CheckResult{}
	}
	if old, ok := m.observations[r.Host]; !ok || !old.CheckedAt.After(r.CheckedAt) {
		m.observations[r.Host] = r
	}
	if m.observationStats == nil {
		m.observationStats = map[string]*hostStats{}
	}
	st, ok := m.observationStats[r.Host]
	if !ok || st.profileSHA != r.ProfileSHA {
		st = &hostStats{profileSHA: r.ProfileSHA}
		m.observationStats[r.Host] = st
	}
	if r.Status == "passed" {
		st.pass++
		st.latency = r.LatencyMS
	} else {
		st.fail++
	}
	st.updated = r.CheckedAt
	if st.updated.IsZero() {
		st.updated = time.Now()
	}
}

// hostStats 是某 hostname 在观察窗口内的通过/失败计数，用于失败率排序。
// 计数随配置变化重置、随过期清理，不做跨配置累积。
type hostStats struct {
	profileSHA string
	pass       int
	fail       int
	latency    float64
	updated    time.Time
}

func (s *hostStats) failRate() float64 {
	total := s.pass + s.fail
	if total == 0 {
		return 0
	}
	return float64(s.fail) / float64(total)
}

// observationFailRateLocked 返回某候选在当前配置下的历史失败率，无记录为 0。
// 调用方必须已持 m.mu。
func (m *Manager) observationFailRateLocked(host, profileSHA string) float64 {
	st, ok := m.observationStats[host]
	if !ok || st.profileSHA != profileSHA {
		return 0
	}
	return st.failRate()
}

func (m *Manager) observationTTLValueLocked() time.Duration {
	if m.observationTTL > 0 {
		return m.observationTTL
	}
	return time.Duration(defaultObservationTTLMinutes) * time.Minute
}

func (m *Manager) observationPerfTTLValueLocked() time.Duration {
	if m.observationPerfTTL > 0 {
		return m.observationPerfTTL
	}
	return time.Duration(defaultPerfTTLMinutes) * time.Minute
}

func (m *Manager) connectivityRank(n Node) (int, float64) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.connectivityRankLocked(n, time.Now())
}

func (m *Manager) connectivityRankLocked(n Node, now time.Time) (int, float64) {
	r, ok := m.observations[n.HostName]
	if !ok || r.ProfileSHA != profileDigest(n.Config) || r.CheckedAt.IsZero() || now.Sub(r.CheckedAt) < 0 || now.Sub(r.CheckedAt) > m.observationTTLValueLocked() {
		return 1, 0
	}
	if r.Status == "passed" {
		// 档位用 30 分钟窗口，档内延迟排序只认 10 分钟内的实测：
		// 过期的延迟按未知处理，排在新鲜实测之后、未测之前。
		latency := 0.0
		if now.Sub(r.CheckedAt) <= m.observationPerfTTLValueLocked() {
			latency = r.LatencyMS
		}
		return 0, latency
	}
	if r.Status == "failed" {
		return 2, 0
	}
	return 1, 0
}

func (m *Manager) pruneConnectivity(now time.Time, ttl time.Duration, allowed map[string]bool) {
	if ttl <= 0 {
		ttl = time.Duration(defaultObservationTTLMinutes) * time.Minute
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for host, result := range m.observations {
		if result.CheckedAt.IsZero() || result.CheckedAt.After(now) || now.Sub(result.CheckedAt) > ttl ||
			(len(allowed) > 0 && !allowed[host]) {
			delete(m.observations, host)
		}
	}
	for host, st := range m.observationStats {
		if st.updated.IsZero() || st.updated.After(now) || now.Sub(st.updated) > ttl ||
			(len(allowed) > 0 && !allowed[host]) {
			delete(m.observationStats, host)
		}
	}
}

func (m *Manager) pruneLocalEvidence(now time.Time, allowed map[string]bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for host, node := range m.localEvidence {
		if len(allowed) > 0 && !allowed[host] {
			delete(m.localEvidence, host)
			continue
		}
		image := node.OpenVPNEgress
		if image == nil {
			delete(m.localEvidence, host)
			continue
		}
		expiry, err := time.Parse(time.RFC3339Nano, image.ExpiresAt)
		if err != nil || !expiry.After(now) {
			delete(m.localEvidence, host)
			continue
		}
		queried, err := time.Parse(time.RFC3339Nano, image.QueriedAt)
		if err != nil || now.Sub(queried) > 24*time.Hour {
			delete(m.localEvidence, host)
		}
	}
}
