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
}

func (m *Manager) observationTTLValueLocked() time.Duration {
	if m.observationTTL > 0 {
		return m.observationTTL
	}
	return time.Duration(defaultObservationTTLMinutes) * time.Minute
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
		return 0, r.LatencyMS
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
