package main

import (
	"fmt"
	"time"
)

const (
	defaultObservationTTLMinutes = 30
	defaultIPPureTTLHours        = 24
	minCheckResultTTLMinutes     = 30
	maxCheckResultTTLMinutes     = 24 * 60
	maxObservationTTLMinutes     = 24 * 60
	maxIPPureTTLHours            = 24 * 7
	minAutomaticResultTTL        = 90 * time.Minute
	maxAutomaticResultTTL        = 24 * time.Hour
	// A batch checks candidates one after another. Result validity has to cover
	// the round that writes the result, the wait until the next round and that
	// whole next round, otherwise evidence expires before it is refreshed.
	resultTTLGrace            = 15 * time.Minute
	cycleEstimatePerCandidate = 30 * time.Second
	maxCycleEstimate          = 120 * time.Minute
)

// automaticCheckResultTTL derives validity from the saved schedule and the
// measured (or estimated) batch duration:
//
//	interval + 2 x cycle + grace
//
// clamped so a fast schedule cannot leave results permanently valid and a slow
// one cannot expire them mid-batch.
func automaticCheckResultTTL(enabled bool, checkMinutes int, cycle time.Duration) time.Duration {
	if !enabled {
		return minAutomaticResultTTL
	}
	ttl := time.Duration(checkMinutes)*time.Minute + 2*cycle + resultTTLGrace
	if ttl < minAutomaticResultTTL {
		return minAutomaticResultTTL
	}
	if ttl > maxAutomaticResultTTL {
		return maxAutomaticResultTTL
	}
	return ttl
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

func configuredResultTTL(minutes int) time.Duration {
	if minutes <= 0 {
		return 0
	}
	ttl := time.Duration(minutes) * time.Minute
	if ttl < minCheckResultTTLMinutes*time.Minute {
		return minCheckResultTTLMinutes * time.Minute
	}
	if ttl > maxCheckResultTTLMinutes*time.Minute {
		return maxCheckResultTTLMinutes * time.Minute
	}
	return ttl
}

// effectiveResultTTL prefers the operator override but never lets it expire a
// result before the next scheduled round could refresh it.
func effectiveResultTTL(configuredMinutes int, enabled bool, checkMinutes int, cycle time.Duration) time.Duration {
	if ttl := configuredResultTTL(configuredMinutes); ttl > 0 {
		if enabled {
			if floor := time.Duration(checkMinutes)*time.Minute + resultTTLGrace; floor > ttl {
				return floor
			}
		}
		return ttl
	}
	return automaticCheckResultTTL(enabled, checkMinutes, cycle)
}

// effectiveObservationTTL keeps ordering observations usable for a whole batch
// plus the following wait, without ever outliving the result they describe.
func effectiveObservationTTL(configuredMinutes int, resultTTL, interval, cycle time.Duration) time.Duration {
	minutes := configuredMinutes
	if minutes <= 0 {
		minutes = defaultObservationTTLMinutes
	}
	if minutes > maxObservationTTLMinutes {
		minutes = maxObservationTTLMinutes
	}
	ttl := time.Duration(minutes) * time.Minute
	if want := interval + cycle + resultTTLGrace; want > ttl {
		ttl = want
	}
	if resultTTL > 0 && ttl > resultTTL {
		return resultTTL
	}
	return ttl
}

func validateResultTTLForSchedule(resultMinutes int, enabled bool, checkMinutes int) error {
	if resultMinutes <= 0 || !enabled {
		return nil
	}
	floor := time.Duration(checkMinutes)*time.Minute + resultTTLGrace
	if time.Duration(resultMinutes)*time.Minute < floor {
		return fmt.Errorf("结果有效期不能短于检测间隔 + 15 分钟（至少 %d 分钟）", int(floor.Minutes()))
	}
	return nil
}

func (c *Console) automationCheckSchedule() (bool, int) {
	if c.automation == nil {
		return false, 0
	}
	c.automation.mu.Lock()
	defer c.automation.mu.Unlock()
	return c.automation.settings.CheckEnabled, c.automation.settings.CheckMinutes
}

// checkCycleDuration is the larger of the last completed batch and a
// conservative estimate for the current pool, so a single slow round is enough
// to widen validity for the following ones.
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
	c.ttlMu.RLock()
	configured := c.checkResultTTLMinutes
	c.ttlMu.RUnlock()
	enabled, minutes := c.automationCheckSchedule()
	return effectiveResultTTL(configured, enabled, minutes, c.checkCycleDuration())
}

func (c *Console) observationTTLAt(now time.Time) time.Duration {
	c.ttlMu.RLock()
	configured := c.observationTTLMinutes
	c.ttlMu.RUnlock()
	enabled, minutes := c.automationCheckSchedule()
	interval, cycle := time.Duration(0), time.Duration(0)
	if enabled {
		interval = time.Duration(minutes) * time.Minute
		cycle = c.checkCycleDuration()
	}
	return effectiveObservationTTL(configured, c.checkResultTTLAt(now), interval, cycle)
}

func (c *Console) applyTTLSettings(now time.Time) {
	observation := c.observationTTLAt(now)
	c.ttlMu.RLock()
	hours := c.ippureTTLHours
	c.ttlMu.RUnlock()
	if hours <= 0 {
		hours = defaultIPPureTTLHours
	}
	if hours > maxIPPureTTLHours {
		hours = maxIPPureTTLHours
	}
	c.m.setIPPureTTL(time.Duration(hours) * time.Hour)
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
