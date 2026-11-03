package main

import "time"

const (
	defaultObservationTTLMinutes = 30
	defaultIPPureTTLHours        = 24
	minCheckResultTTLMinutes     = 1
	maxCheckResultTTLMinutes     = 24 * 60
	maxObservationTTLMinutes     = 24 * 60
	maxIPPureTTLHours            = 24 * 7
	minAutomaticResultTTL        = 30 * time.Minute
	maxAutomaticResultTTL        = 24 * time.Hour
)

// automaticCheckResultTTL is the saved automatic-check interval plus a
// fifteen-minute grace, clamped so a slow schedule cannot leave results
// expired between rounds and a fast one cannot make evidence permanent.
func automaticCheckResultTTL(enabled bool, checkMinutes int) time.Duration {
	if !enabled {
		return minAutomaticResultTTL
	}
	ttl := time.Duration(checkMinutes)*time.Minute + 15*time.Minute
	if ttl < minAutomaticResultTTL {
		return minAutomaticResultTTL
	}
	if ttl > maxAutomaticResultTTL {
		return maxAutomaticResultTTL
	}
	return ttl
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

func effectiveResultTTL(configuredMinutes int, enabled bool, checkMinutes int) time.Duration {
	if ttl := configuredResultTTL(configuredMinutes); ttl > 0 {
		return ttl
	}
	return automaticCheckResultTTL(enabled, checkMinutes)
}

func effectiveObservationTTL(configuredMinutes int, resultTTL time.Duration) time.Duration {
	minutes := configuredMinutes
	if minutes <= 0 {
		minutes = defaultObservationTTLMinutes
	}
	if minutes > maxObservationTTLMinutes {
		minutes = maxObservationTTLMinutes
	}
	ttl := time.Duration(minutes) * time.Minute
	if resultTTL > 0 && ttl > resultTTL {
		return resultTTL
	}
	return ttl
}

func (c *Console) checkResultTTLAt(now time.Time) time.Duration {
	c.ttlMu.RLock()
	configured := c.checkResultTTLMinutes
	c.ttlMu.RUnlock()
	enabled, minutes := false, 0
	if c.automation != nil {
		c.automation.mu.Lock()
		enabled, minutes = c.automation.settings.CheckEnabled, c.automation.settings.CheckMinutes
		c.automation.mu.Unlock()
	}
	return effectiveResultTTL(configured, enabled, minutes)
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
