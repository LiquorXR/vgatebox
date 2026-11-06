package main

// manager.go：Manager 结构与构造；只读选择器见 manager_selectors.go，生命周期见 manager_lifecycle.go。
import (
	"context"
	"fanout/internal/store"
	"sync"
	"time"
)

// Manager 维护所有隧道，负责分配槽位与端口。
type Manager struct {
	mu              sync.RWMutex
	tunnels         map[int]*Tunnel
	nodes           []Node
	fetched         time.Time
	workDir         string
	evidenceDir     string
	maxSlots        int
	jobs            JobStore
	saveMu          sync.Mutex
	shuttingDown    bool // protected by saveMu; transient teardown is not user intent
	deletedSlots    map[int]bool
	localEvidence   map[string]Node
	stateWriteHook  func(string) error
	credentialApply func(*Tunnel) error
	connectBudget   *connectBudget
	observations    map[string]CheckResult
	observationTTL  time.Duration
	diagnostics     map[string]*diagnosticLease
	console         *Console

	// Health probing is bounded and test seams use the same paths as production.
	healthEvery         time.Duration
	healthParallelLimit int
	healthProbeWindow   time.Duration
	healthEchoWindow    time.Duration
	healthOpWindow      time.Duration
	healthProbe         func(context.Context, *Tunnel) (string, float64, error)
	healthReconnect     func(*Tunnel, string)

	ippureMu          sync.Mutex
	ippureCache       map[string]ippureCacheEntry
	ippureCacheLoaded bool
	ippureHits        int
	ippureQueries     int
	ippureTTL         time.Duration

	// slotHealthDoc 存出口观察快照：SQLite 可用时走共享库的 slot_health
	// 文档，无库（单测裸 Manager）时回退旧文件。快照可再生，fail-open。
	slotHealthDoc store.DocStore
}

func NewManager(maxSlots int, workDir string) *Manager {
	return &Manager{
		tunnels:       map[int]*Tunnel{},
		workDir:       workDir,
		evidenceDir:   workDir,
		maxSlots:      maxSlots,
		deletedSlots:  map[int]bool{},
		localEvidence: map[string]Node{},
		connectBudget: newConnectBudget(defaultCheckParallelism),
		observations:  map[string]CheckResult{},
		diagnostics:   map[string]*diagnosticLease{},
		ippureCache:   map[string]ippureCacheEntry{},
		ippureTTL:     defaultIPPureTTL,
	}
}
