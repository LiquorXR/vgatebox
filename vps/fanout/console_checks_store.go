package main

import (
	"encoding/json"
	"fanout/internal/paths"
	"fanout/internal/store"
	"fanout/internal/storesqlite"
	"fmt"
	"log"
	"os"
	"sort"
	"time"
)

const (
	defaultChecksFlushInterval = 2 * time.Second
	maxStoredCheckResults      = 1000
)

// openCheckStore 打开检测结果存储：SQLite 优先，打不开回退旧 JSON 文件。
// 回退读写旧形状（map[host]CheckResult），不断旧库的连续性。
func openCheckStore(workDir string) (store.MapStore, *storesqlite.DB) {
	if db, err := storesqlite.Open(paths.DBFile(workDir)); err != nil {
		log.Printf("检测结果改用文件存储: %v", err)
		return legacyChecksFile{path: paths.Checks(workDir)}, nil
	} else {
		return db.MapStore("checks"), db
	}
}

// legacyChecksFile 是 Store 接口的旧形状适配：文件里是 map[host]CheckResult，
// 进出时与 store.Entry 互转。只在 SQLite 打不开时使用。
type legacyChecksFile struct{ path string }

func (s legacyChecksFile) LoadAll() (map[string]store.Entry, error) {
	out := map[string]store.Entry{}
	blob, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return nil, err
	}
	var m map[string]CheckResult
	if err := json.Unmarshal(blob, &m); err != nil || m == nil {
		return nil, fmt.Errorf("invalid checks file: %w", err)
	}
	for host, r := range m {
		data, err := json.Marshal(r)
		if err != nil {
			return nil, err
		}
		out[host] = store.Entry{Data: data, CheckedAt: r.CheckedAt, ExpiresAt: r.ExpiresAt}
	}
	return out, nil
}

func (s legacyChecksFile) SaveAll(m map[string]store.Entry) error {
	out := make(map[string]CheckResult, len(m))
	for host, e := range m {
		var r CheckResult
		if err := json.Unmarshal(e.Data, &r); err != nil {
			return err
		}
		out[host] = r
	}
	blob, err := json.Marshal(out)
	if err != nil {
		return err
	}
	return atomicConfigWrite(s.path, blob)
}

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
	entries := make(map[string]store.Entry, len(c.results))
	for host, r := range c.results {
		blob, err := json.Marshal(r)
		if err != nil {
			c.checksMu.Unlock()
			return err
		}
		entries[host] = store.Entry{Data: blob, CheckedAt: r.CheckedAt, ExpiresAt: r.ExpiresAt}
	}
	c.checksMu.Unlock()
	if err := c.checkStore.SaveAll(entries); err != nil {
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
