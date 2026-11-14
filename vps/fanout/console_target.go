package main

// console_target.go：出口目标对账（applyTarget/reconcile/deleteSlot/watch）。
import (
	"fmt"
	"log"
	"sort"
	"time"
)

// Allocate intent immediately, then let bounded workers validate it. Existing
// Slots keep their own policies. Missing candidates remain allocated/waiting.
func (c *Console) applyTarget(p ExitPlan) error {
	// Public target changes cannot inject an internal interrupted deletion.
	p.RemoveSlot = 0
	if err := validateExitPlan(&p, c.m.maxSlots); err != nil {
		return err
	}
	c.targetMu.Lock()
	defer c.targetMu.Unlock()
	if c.ctx.Err() != nil {
		return fmt.Errorf("管理服务正在停止")
	}
	if err := c.savePlan(p); err != nil {
		return fmt.Errorf("无法保存出口计划")
	}
	return c.reconcile()
}

func (c *Console) reconcile() error {
	// planError 是 apiConsole 可见字段：变化才递增版本号，避免 15s 空转对账
	// 触发前端全量拉取。调用方均持有 targetMu，读写安全。
	prevPlanError := c.planError
	defer func() {
		if c.planError != prevPlanError {
			c.bumpConsoleVersion()
		}
	}()
	if c.ctx.Err() != nil {
		return c.ctx.Err()
	}
	if err := c.initializeDefaultTarget(); err != nil {
		c.planError = "default_initialization_failed"
		return err
	}
	if c.plan == nil {
		return nil
	}
	p := *c.plan
	if p.RemoveSlot > 0 {
		for _, t := range c.m.Tunnels() {
			if t.Slot == p.RemoveSlot {
				if err := c.remove(t.Slot); err != nil {
					c.planError = "remove_failed"
					return err
				}
			}
		}
		p.RemoveSlot = 0
		if err := c.savePlan(p); err != nil {
			return err
		}
	}
	ts := c.m.Tunnels()
	sort.Slice(ts, func(i, j int) bool { return ts[i].Slot > ts[j].Slot })
	for len(ts) > p.Count {
		if err := c.remove(ts[0].Slot); err != nil {
			c.planError = "remove_failed"
			return err
		}
		ts = ts[1:]
	}
	noVolunteers := false
	for len(c.m.Tunnels()) < p.Count {
		node := Node{CountryCode: p.Policy.Country, Country: p.Policy.Country}
		if candidates := c.m.policyCandidates(p.Policy, nil, 0); len(candidates) > 0 {
			node = candidates[0]
		} else if residentialOnly() {
			if volunteers, total := c.m.volunteerPoolStats(); total > 0 && volunteers == 0 {
				noVolunteers = true
			}
		}
		if _, err := c.start(node, p.Policy); err != nil {
			c.planError = "allocation_failed"
			return err
		}
	}
	if err := c.ensure(c.m.Tunnels(), p.ServerName); err != nil {
		c.planError = "auto_binding_failed"
		return err
	}
	if noVolunteers {
		c.planError = "no_volunteer_candidates"
		log.Printf("自动选点等待: 节点池没有志愿者家宽节点；可在设置里关闭「只使用志愿者家宽」")
	} else {
		c.planError = ""
	}
	invalidateInbounds()
	return nil
}

func (c *Console) deleteSlot(slot int) error {
	c.targetMu.Lock()
	defer c.targetMu.Unlock()
	c.m.mu.RLock()
	exists := c.m.tunnels[slot] != nil
	c.m.mu.RUnlock()
	if !exists {
		return fmt.Errorf("出口不存在")
	}
	// A final explicit deletion of legacy Slots must not become an untouched
	// instance that the new default would initialize again on the next refresh.
	if c.plan == nil && len(c.m.Tunnels()) == 1 && currentPanelMode() == "native" {
		p := ExitPlan{Count: 0, RemoveSlot: slot, Policy: defaultSlotPolicyForMode("", false)}
		if err := validateExitPlan(&p, c.m.maxSlots); err != nil {
			return err
		}
		if err := c.savePlan(p); err != nil {
			return err
		}
		return c.reconcile()
	}
	// Save the new target before deletion; a crash resumes the explicit decrease.
	if c.plan != nil {
		p := *c.plan
		p.Count = max(0, c.plan.Count-1)
		p.RemoveSlot = slot
		if p.Count < 0 {
			return fmt.Errorf("出口不存在")
		}
		if err := c.savePlan(p); err != nil {
			return err
		}
	}
	if c.plan != nil {
		return c.reconcile()
	}
	return c.m.deleteSlot(slot)
}

func (c *Console) watch() {
	for {
		if c.ctx.Err() != nil {
			return
		}
		c.targetMu.Lock()
		if err := c.reconcile(); err != nil {
			log.Printf("出口计划等待重试: %v", err)
		}
		c.targetMu.Unlock()
		select {
		case <-c.ctx.Done():
			return
		case <-c.poolChanged:
		case <-time.After(15 * time.Second):
		}
	}
}
