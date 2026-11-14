package main

// console_check_run.go：批量检测编排（startCheck/runChecks/dispatch）。
import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"sync"
	"time"
)

type CheckRequest struct {
	scheduled   bool
	Kind        string   `json:"kind"`
	Hosts       []string `json:"hosts"`
	Slots       []int    `json:"slots"`
	Parallelism int      `json:"parallelism,omitempty"`
	Priority    bool     `json:"priority,omitempty"`
	All         bool     `json:"all,omitempty"`
}

func checkItemKey(i CheckResult) string {
	if i.Host != "" {
		return "host:" + i.Host
	}
	return fmt.Sprintf("slot:%d", i.Slot)
}

func (c *Console) startCheck(in CheckRequest) (*CheckRun, error) {
	if in.Kind != "" && in.Kind != "checks" && in.Kind != "candidates" && in.Kind != "exits" {
		return nil, fmt.Errorf("检测类型无效")
	}
	nodes, _ := c.m.Nodes()
	byHost := map[string]Node{}
	for _, n := range nodes {
		byHost[n.HostName] = n
	}
	if in.All {
		for _, n := range nodes {
			in.Hosts = append(in.Hosts, n.HostName)
		}
	}
	items := []CheckResult{}
	seen := map[string]bool{}
	add := func(i CheckResult) {
		k := checkItemKey(i)
		if !seen[k] {
			seen[k] = true
			i.Status = "queued"
			i.Priority = in.Priority
			items = append(items, i)
		}
	}
	for _, host := range in.Hosts {
		n, ok := byHost[host]
		if !ok {
			return nil, fmt.Errorf("候选已从当前池消失，请刷新列表")
		}
		add(CheckResult{Host: host, ProfileSHA: profileDigest(n.Config)})
	}
	for _, slot := range in.Slots {
		c.m.mu.RLock()
		t := c.m.tunnels[slot]
		c.m.mu.RUnlock()
		if t == nil {
			return nil, fmt.Errorf("出口不存在")
		}
		v := t.snapshot()
		add(CheckResult{Slot: slot, Host: v.Node.HostName, ProfileSHA: profileDigest(v.Node.Config)})
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("请选择要检测的节点")
	}
	c.checksMu.Lock()
	defer c.checksMu.Unlock()
	if c.ctx.Err() != nil {
		return nil, fmt.Errorf("管理服务正在停止")
	}
	checkIPType := c.checkIPType
	if in.Parallelism == 0 {
		in.Parallelism = c.checkParallelism
	}
	if err := validateCheckParallelism(in.Parallelism); err != nil {
		return nil, err
	}
	if c.check != nil {
		j := c.check
		j.mu.Lock()
		if j.Status == "running" {
			if !in.Priority || j.ctx.Err() != nil {
				j.mu.Unlock()
				return nil, fmt.Errorf("已有检测任务，请等待或取消")
			}
			for _, item := range items {
				duplicate := false
				for i := range j.Items {
					old := &j.Items[i]
					if checkItemKey(*old) == checkItemKey(item) && (old.Status == "queued" || old.Status == "running") {
						old.Priority = true
						duplicate = true
						break
					}
				}
				if !duplicate {
					j.Items = append(j.Items, item)
					j.Total++
				}
			}
			for k, n := range byHost {
				j.nodes[k] = n
			}
			j.mu.Unlock()
			select {
			case j.wake <- struct{}{}:
			default:
			}
			c.bumpConsoleVersion()
			return j, nil
		}
		j.mu.Unlock()
	}
	if in.Parallelism != c.checkParallelism {
		if err := c.saveCheckSettings(in.Parallelism, checkIPType); err != nil {
			return nil, err
		}
	}
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(c.ctx)
	if err := c.automationCheckStarted(in.scheduled); err != nil {
		cancel()
		return nil, err
	}
	job := &CheckRun{ID: hex.EncodeToString(buf), Status: "running", Kind: "checks", Items: items, Total: len(items), Parallelism: in.Parallelism, IPTypeCheck: checkIPType, poolGen: currentPoolGeneratedAt(), cancel: cancel, ctx: ctx, finished: make(chan struct{}), wake: make(chan struct{}, 1), nodes: byHost}
	c.check = job
	c.bumpConsoleVersion()
	go c.runChecks(ctx, job)
	return job, nil
}

// One dispatcher owns all running diagnostic identities. Manual items can join
// a batch and take the next free permit without exceeding its configured limit.
func (c *Console) runChecks(ctx context.Context, j *CheckRun) {
	// The final flush must complete before j.finished closes, so a finished task
	// is durable and tests or shutdown never race a late write.
	dispatchCheckRun(ctx, j, &c.checksMu, func(index, slot int, item CheckResult, node Node) { c.runCheckItem(ctx, j, index, item, node, slot) }, func() {
		c.automationCheckFinished(j)
		// The finished batch is the best cycle sample; refresh derived TTLs so
		// ordering observations survive the next round too.
		c.applyTTLSettings(time.Now().UTC())
		if err := c.flushChecks(); err != nil {
			log.Printf("检测结果写入失败: %v", err)
		}
	})
}

func dispatchCheckRun(ctx context.Context, j *CheckRun, ownerMu *sync.Mutex, runItem func(int, int, CheckResult, Node), onComplete ...func()) {
	defer close(j.finished)
	defer j.cancel()
	if len(onComplete) > 0 {
		defer onComplete[0]()
	}
	finished := make(chan int, j.Parallelism)
	active := map[int]bool{}
	cancelSignal := ctx.Done()
	for {
		j.mu.Lock()
		for len(active) < j.Parallelism && ctx.Err() == nil {
			next := -1
			for i, item := range j.Items {
				if item.Status == "queued" && (next < 0 || item.Priority && !j.Items[next].Priority) {
					next = i
				}
			}
			if next < 0 {
				break
			}
			slot := diagnosticFirstSlot
			for active[slot] {
				slot--
			}
			active[slot] = true
			j.Items[next].Status = "running"
			j.Items[next].StartedAt = time.Now().UTC()
			item := j.Items[next]
			node := j.nodes[item.Host]
			go func(index, slot int, item CheckResult, node Node) {
				runItem(index, slot, item, node)
				finished <- slot
			}(next, slot, item, node)
		}
		if ctx.Err() != nil {
			for i := range j.Items {
				if j.Items[i].Status == "queued" {
					j.Items[i].Status = "cancelled"
					j.Done++
				}
			}
		}
		idle := len(active) == 0
		j.mu.Unlock()
		if idle {
			// Serialize completion with startCheck's append, so no late manual item
			// can be accepted after the last worker has gone away.
			ownerMu.Lock()
			j.mu.Lock()
			queued := false
			for _, i := range j.Items {
				if i.Status == "queued" {
					queued = true
					break
				}
			}
			if !queued {
				j.Status = "done"
				if ctx.Err() != nil {
					j.Status = "cancelled"
				}
				j.mu.Unlock()
				ownerMu.Unlock()
				return
			}
			j.mu.Unlock()
			ownerMu.Unlock()
			continue
		}
		select {
		case slot := <-finished:
			delete(active, slot)
		case <-j.wake:
		case <-cancelSignal:
			cancelSignal = nil
		}
	}
}

func (c *Console) runCheckItem(ctx context.Context, j *CheckRun, index int, item CheckResult, node Node, slot int) {
	progress := func(phase string) { j.mu.Lock(); j.Items[index].Phase = phase; j.mu.Unlock() }
	var result CheckResult
	if item.Slot > 0 {
		result = c.checkExit(ctx, item.Slot, item.Host, progress)
	} else {
		reused := false
		for _, t := range c.m.Tunnels() {
			v := t.snapshot()
			if v.Node.HostName == item.Host && v.Status == "up" {
				result = c.checkExit(ctx, v.Slot, item.Host, progress)
				reused = true
				break
			}
		}
		if !reused {
			probeCtx := context.WithValue(ctx, checkIPTypeContextKey{}, j.IPTypeCheck)
			result = c.probe(probeCtx, node, slot, progress)
		}
	}
	result.CheckedAt = time.Now().UTC()
	result.StartedAt = item.StartedAt
	result.Priority = item.Priority
	if result.Protocol == "" || result.Protocol == "openvpn" {
		result.ExpiresAt = result.CheckedAt.Add(c.checkResultTTLAt(result.CheckedAt))
	}
	if result.Host == "" {
		result.Host = item.Host
	}
	if result.Slot == 0 && item.Slot > 0 {
		result.Slot = item.Slot
	}
	if ctx.Err() != nil {
		result.Status, result.Code = "cancelled", "cancelled"
	}
	// Only completed observations replace previous evidence; no count-based
	// truncation discards a large user's results. Persistence is coalesced and
	// happens outside this worker.
	if (result.Status == "passed" || result.Status == "failed") && result.Host != "" {
		c.checksMu.Lock()
		c.results[result.Host] = result
		c.m.recordConnectivity(result)
		c.markChecksDirtyLocked()
		c.checksMu.Unlock()
	}
	j.mu.Lock()
	j.Items[index] = result
	j.Done++
	j.mu.Unlock()
}
