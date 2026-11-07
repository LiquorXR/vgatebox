package main

// console.go：Console 结构、构造、出口计划存取与版本计数。
import (
	"context"
	"encoding/json"
	"fanout/internal/paths"
	"fanout/internal/store"
	"fanout/internal/storesqlite"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const consoleJournal = ".console-transaction.json"

type ExitPlan struct {
	SchemaVersion int        `json:"schema_version,omitempty"`
	Count         int        `json:"count"`
	Policy        SlotPolicy `json:"policy"`
	ServerName    string     `json:"server_name"`
	RemoveSlot    int        `json:"remove_slot,omitempty"`
}

type Console struct {
	third                 *ThirdPartyManager
	sstp                  *SstpManager
	ctx                   context.Context
	cancel                context.CancelFunc
	refreshDone           chan struct{}
	m                     *Manager
	targetMu              sync.Mutex
	plan                  *ExitPlan
	planError             string
	refreshMu             sync.Mutex
	refreshState          string
	refreshAt             string
	refreshError          string
	refreshCause          string
	refreshStarted        time.Time
	sourceMu              sync.Mutex
	poolChanged           chan struct{}
	automation            *automationManager
	checksMu              sync.Mutex
	check                 *CheckRun
	results               map[string]CheckResult
	checkParallelism      int
	checkIPType           bool
	observationTTLMinutes int
	ippureTTLHours        int
	ttlMu                 sync.RWMutex
	checksDirty           bool
	checksFlushInterval   time.Duration
	// version 是控制台可见状态的单调版本号，供 /api/console/version 轮询：
	// 前端先轮询小版本号，变化时才拉全量 /api/console。
	// 所有 apiConsole 可见的变更点都必须 bump（savePlan、planError 变化、
	// saveState/RefreshNodes 经 m.console、检测启停与结果、刷新启停、自动化与检测设置）。
	// 只增不减，atomic 保证并发安全，调用方无需持锁。
	version atomic.Uint64
	// checkStore 存检测结果：SQLite 优先（state/vgatebox.db），打不开
	// 则回退同目录旧 checks.json（legacyChecksFile，读写旧形状）。
	// 旧库升级上来没有历史也无妨：结果会随下一轮检测重建。
	checkStore store.MapStore
	checkDB    *storesqlite.DB
	// Test seams exercise intent/persistence without creating real namespaces.
	start  func(Node, SlotPolicy) (*Tunnel, error)
	remove func(int) error
	ensure func([]*Tunnel, string) error
	probe  func(context.Context, Node, int, func(string)) CheckResult
}

func newConsole(m *Manager) (*Console, error) {
	// 测试夹具直接拿裸 TempDir 当 workDir：这里建好子目录，
	// 后续原子写（临时文件建在目标同目录）才有父目录可用。
	if err := paths.Ensure(m.workDir); err != nil {
		return nil, err
	}
	c := &Console{m: m, results: map[string]CheckResult{}, start: m.StartWithPolicy, remove: m.deleteSlot, poolChanged: make(chan struct{}, 1)}
	c.ctx, c.cancel = context.WithCancel(context.Background())
	c.ensure = func(ts []*Tunnel, host string) error {
		p, err := openPanel()
		if err != nil {
			return err
		}
		n, ok := p.(*Native)
		if !ok {
			return fmt.Errorf("自动数量管理仅用于内置 Xray 模式")
		}
		return n.ensureSlotInbounds(ts, host)
	}
	c.probe = c.probeCandidate
	if err := recoverConfigTransaction(paths.StateDir(m.workDir), consoleJournal, []string{"console.json"}); err != nil {
		return nil, err
	}
	blob, err := os.ReadFile(paths.Console(m.workDir))
	if err == nil {
		var plan *ExitPlan
		if json.Unmarshal(blob, &plan) != nil || plan == nil {
			return nil, fmt.Errorf("出口计划损坏，保留原文件")
		}
		if err := validateExitPlan(plan, m.maxSlots); err != nil {
			return nil, err
		}
		c.plan = plan
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	// Observations are auxiliary; they never restore a tunnel as healthy.
	// 检测结果走 Store：SQLite 优先，打不开回退旧 JSON 文件。
	// 旧库没有历史也无妨，结果随下一轮检测重建（fail-open 只适用于
	// 这种可再生的辅助数据，意图类状态仍是 fail-closed，见 ADR-0007）。
	c.checkStore, c.checkDB = openCheckStore(m.workDir)
	if c.checkDB != nil {
		m.slotHealthDoc = c.checkDB.DocStore("slot_health")
		m.ippureMap = c.checkDB.MapStore("ippure")
	} else {
		m.slotHealthDoc = store.FileDocStore{Path: paths.SlotHealth(m.workDir)}
	}
	if entries, err := c.checkStore.LoadAll(); err != nil {
		log.Printf("检测结果读取失败，从空开始: %v", err)
	} else {
		for host, e := range entries {
			var r CheckResult
			if json.Unmarshal(e.Data, &r) != nil {
				log.Printf("检测结果 %q损坏已跳过", host)
				continue
			}
			c.results[host] = r
		}
	}
	if c.results == nil {
		c.results = map[string]CheckResult{}
	}
	if err := c.loadCheckSettings(); err != nil {
		return nil, err
	}
	if err := c.loadAutomation(); err != nil {
		c.cancel()
		return nil, err
	}
	now := time.Now().UTC()
	c.applyTTLSettings(now)
	c.migrateCheckResultTTL(now)
	for _, r := range c.results {
		m.recordConnectivity(r)
	}
	c.retainChecks(now)
	if err := c.loadSstpManager(); err != nil {
		c.cancel()
		return nil, err
	}
	c.loadThirdPartyManager()
	m.console = c
	return c, nil
}

func validateExitPlan(p *ExitPlan, limit int) error {
	if p == nil {
		return fmt.Errorf("出口计划缺失")
	}
	if err := validatePersistedSchemaVersion(p.SchemaVersion, "console.json"); err != nil {
		return err
	}
	if p.Count < 0 || p.Count > limit || p.Count > maximumSlots {
		return fmt.Errorf("出口数量须为非负整数，不能超过本实例地址/管理员容量 %d", limit)
	}
	if p.Count > 0 {
		if _, _, err := fixedSlotPort(p.Count); err != nil {
			return err
		}
	}
	if p.RemoveSlot < 0 || p.RemoveSlot > limit {
		return fmt.Errorf("待移除出口编号无效")
	}
	policy, err := normalizePolicy(p.Policy)
	if err != nil {
		return err
	}
	p.Policy = policy
	p.ServerName = strings.ToLower(strings.TrimSpace(p.ServerName))
	if p.ServerName == "" {
		p.ServerName = "www.tesla.com"
	}
	if len(p.ServerName) > 253 || !strings.Contains(p.ServerName, ".") || !regexp.MustCompile(`^[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?$`).MatchString(p.ServerName) {
		return fmt.Errorf("REALITY 目标须为有效域名")
	}
	return nil
}

// bumpConsoleVersion 递增控制台可见状态版本号。atomic 实现，调用方无需持锁。
func (c *Console) bumpConsoleVersion() { c.version.Add(1) }

// consoleVersion 返回当前版本号，供 /api/console/version 与 apiConsole 应答。
func (c *Console) consoleVersion() uint64 { return c.version.Load() }

func (c *Console) savePlan(p ExitPlan) error {
	p.SchemaVersion = persistedSchemaVersion
	blob, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	tx, err := beginConfigTransaction(paths.StateDir(c.m.workDir), consoleJournal, []string{"console.json"})
	if err != nil {
		return err
	}
	if err := atomicConfigWrite(paths.Console(c.m.workDir), blob); err != nil {
		_ = tx.rollback()
		return err
	}
	if err := tx.commit(); err != nil {
		_ = tx.rollback()
		return err
	}
	c.plan = &p
	c.bumpConsoleVersion()
	return nil
}
