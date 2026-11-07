package main

import (
	"time"
)

const (
	defaultObservationTTLMinutes = 30
	defaultIPTypeTTLHours        = 12
	maxObservationTTLMinutes     = 24 * 60
	maxIPTypeTTLHours            = 24 * 7
	// 检测结果有效期固定 30 分钟：只决定展示与排序引用是否标过期，
	// 自动出口建连时照常做完整 live 验证，不依赖该有效期选点。
	fixedCheckResultTTLMinutes = 30
	// A batch checks candidates one after another. 排序观察窗口至少覆盖
	// 一次检测列举与写入的耗时，其余由用户配置与结果有效期上限决定。
	resultTTLGrace            = 15 * time.Minute
	cycleEstimatePerCandidate = 30 * time.Second
	maxCycleEstimate          = 120 * time.Minute
)

// effectiveResultTTL 固定返回 30 分钟有效期。
func effectiveResultTTL(configuredMinutes int) time.Duration {
	return time.Duration(fixedCheckResultTTLMinutes) * time.Minute
}

// checkCycleEstimate is the conservative first-run estimate before any batch
// duration has been observed.
func checkCycleEstimate(nodes, parallelism int) time.Duration {
	if nodes <= 0 {
		return 0
	}
	if parallelism < 1 {
		parallelism = 1
	}
	batches := (nodes + parallelism - 1) / parallelism
	estimate := time.Duration(batches) * cycleEstimatePerCandidate
	if estimate > maxCycleEstimate {
		return maxCycleEstimate
	}
	return estimate
}

// effectiveObservationTTL keeps ordering observations usable without ever
// outliving the fixed 30-minute result validity they describe.
func effectiveObservationTTL(configuredMinutes int, resultTTL time.Duration) time.Duration {
	minutes := configuredMinutes
	if minutes <= 0 {
		minutes = defaultObservationTTLMinutes
	}
	if minutes > maxObservationTTLMinutes {
		minutes = maxObservationTTLMinutes
	}
	ttl := time.Duration(minutes) * time.Minute
	if ttl < resultTTLGrace {
		ttl = resultTTLGrace
	}
	if resultTTL > 0 && ttl > resultTTL {
		return resultTTL
	}
	return ttl
}

// checkCycleDuration is the larger of the last completed batch and a
// conservative estimate for the current pool; shown in the console as the
// expected batch length ("单轮检测约 X 分钟").
func (c *Console) checkCycleDuration() time.Duration {
	observed := time.Duration(0)
	if c.automation != nil {
		c.automation.mu.Lock()
		run := c.automation.check
		c.automation.mu.Unlock()
		if !run.StartedAt.IsZero() && run.FinishedAt.After(run.StartedAt) {
			observed = run.FinishedAt.Sub(run.StartedAt)
			if observed > maxCycleEstimate {
				observed = maxCycleEstimate
			}
		}
	}
	estimate := checkCycleEstimate(c.m.NodeCount(), c.m.connectBudget.limitValue())
	if observed > estimate {
		return observed
	}
	return estimate
}

func (c *Console) checkResultTTLAt(now time.Time) time.Duration {
	return effectiveResultTTL(0)
}

func (c *Console) observationTTLAt(now time.Time) time.Duration {
	c.ttlMu.RLock()
	configured := c.observationTTLMinutes
	c.ttlMu.RUnlock()
	return effectiveObservationTTL(configured, c.checkResultTTLAt(now))
}

func (c *Console) applyTTLSettings(now time.Time) {
	observation := c.observationTTLAt(now)
	c.ttlMu.RLock()
	hours := c.ipTypeTTLHours
	c.ttlMu.RUnlock()
	if hours <= 0 {
		hours = defaultIPTypeTTLHours
	}
	if hours > maxIPTypeTTLHours {
		hours = maxIPTypeTTLHours
	}
	c.m.setIPTypeTTL(time.Duration(hours) * time.Hour)
	c.m.mu.Lock()
	c.m.observationTTL = observation
	c.m.mu.Unlock()
}

// migrateCheckResultTTL keeps old checks.json usable. Results written before
// ExpiresAt existed get CheckedAt plus the currently effective TTL; a stored
// expiry is never extended or shortened by a later setting change.
func (c *Console) migrateCheckResultTTL(now time.Time) {
	ttl := c.checkResultTTLAt(now)
	c.checksMu.Lock()
	defer c.checksMu.Unlock()
	for host, result := range c.results {
		if result.CheckedAt.IsZero() {
			continue
		}
		if result.ExpiresAt.IsZero() {
			result.ExpiresAt = result.CheckedAt.Add(ttl)
			c.results[host] = result
			c.checksDirty = true
		}
	}
}

func resultExpiry(result CheckResult, ttl time.Duration) time.Time {
	if !result.ExpiresAt.IsZero() {
		return result.ExpiresAt
	}
	if result.CheckedAt.IsZero() {
		return time.Time{}
	}
	return result.CheckedAt.Add(ttl)
}

// retentionWindow keeps enough history for comparison without treating the
// short result TTL as the storage lifetime.
func retentionWindow(resultTTL time.Duration) time.Duration {
	window := 6 * time.Hour
	if four := 4 * resultTTL; four > window {
		window = four
	}
	if window > 24*time.Hour {
		window = 24 * time.Hour
	}
	return window
}
