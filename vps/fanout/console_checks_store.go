package main

import (
	"encoding/json"
	"log"
	"path/filepath"
	"sort"
	"time"
)

const (
	defaultChecksFlushInterval = 2 * time.Second
	maxStoredCheckResults      = 1000
)

func (c *Console) markChecksDirtyLocked() {
	c.checksDirty = true
	// 检测结果每次变更都递增：运行期进度（passed/失败数）在 apiConsole 可见，
	// 前端 2s 版本轮询据此拉全量，保持与现状一致的实时进度。
	c.bumpConsoleVersion()
}

func (c *Console) checksFlushDelay() time.Duration {
	c.checksMu.Lock()
	delay := c.checksFlushInterval
	c.checksMu.Unlock()
	if delay <= 0 {
		return defaultChecksFlushInterval
	}
	return delay
}

func (c *Console) watchChecksPersistence() {
	for {
		select {
		case <-c.ctx.Done():
			_ = c.flushChecks()
			return
		case <-time.After(c.checksFlushDelay()):
			if err := c.flushChecks(); err != nil {
				log.Printf("检测结果写入失败，稍后重试: %v", err)
			}
		}
	}
}

// flushChecks coalesces all result updates into one atomic write. Callers never
// wait for the disk inside a worker; task completion and shutdown force a
// flush so a finished check is durable even when the process stops.
func (c *Console) flushChecks() error {
	c.checksMu.Lock()
	c.retainChecksLocked(time.Now().UTC())
	if !c.checksDirty {
		c.checksMu.Unlock()
		return nil
	}
	c.checksDirty = false
	blob, err := json.Marshal(c.results)
	c.checksMu.Unlock()
	if err != nil {
		return err
	}
	if err := atomicConfigWrite(filepath.Join(c.m.workDir, "checks.json"), blob); err != nil {
		c.checksMu.Lock()
		c.checksDirty = true
		c.checksMu.Unlock()
		return err
	}
	return nil
}

func (c *Console) retainChecks(now time.Time) {
	c.checksMu.Lock()
	defer c.checksMu.Unlock()
	c.retainChecksLocked(now)
}

func (c *Console) retainChecksLocked(now time.Time) {
	resultTTL := c.checkResultTTLAt(now)
	observationTTL := c.observationTTLAt(now)
	retention := retentionWindow(resultTTL)
	nodes, _ := c.m.Nodes()
	allowed := make(map[string]bool, len(nodes))
	for _, n := range nodes {
		allowed[n.HostName] = true
	}
	changed := false
	for host, result := range c.results {
		// Age is only known for a completed observation; an entry without one is
		// kept until its host leaves the pool instead of being dropped silently.
		if !result.CheckedAt.IsZero() && (result.CheckedAt.After(now) || now.Sub(result.CheckedAt) > retention) {
			delete(c.results, host)
			changed = true
			continue
		}
		if len(allowed) > 0 && !allowed[host] {
			delete(c.results, host)
			changed = true
		}
	}
	if len(c.results) > maxStoredCheckResults {
		hosts := make([]string, 0, len(c.results))
		for host := range c.results {
			hosts = append(hosts, host)
		}
		sort.Slice(hosts, func(i, j int) bool {
			return c.results[hosts[i]].CheckedAt.After(c.results[hosts[j]].CheckedAt)
		})
		for _, host := range hosts[maxStoredCheckResults:] {
			delete(c.results, host)
			changed = true
		}
	}
	if changed {
		c.checksDirty = true
		// 过期/离池裁剪直接改 results，不走 markChecksDirtyLocked，这里补递增。
		c.bumpConsoleVersion()
	}
	c.m.pruneConnectivity(now, observationTTL, allowed)
	c.m.pruneLocalEvidence(now, allowed)
}
