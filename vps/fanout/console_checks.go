package main

// console_checks.go：检测结果类型、单点探测（probeCandidate/checkExit）。
import (
	"context"
	"encoding/json"
	"fanout/internal/paths"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type CheckResult struct {
	Protocol   string         `json:"protocol,omitempty"`
	Egress     *EgressProfile `json:"egress,omitempty"`
	Port       int            `json:"port,omitempty"`
	ExpiresAt  time.Time      `json:"expires_at,omitempty"`
	ObservedBy string         `json:"observed_by,omitempty"`
	CFStatus   string         `json:"cf_status,omitempty"`
	Host       string         `json:"host"`
	Slot       int            `json:"slot,omitempty"`
	Status     string         `json:"status"`
	Phase      string         `json:"phase,omitempty"`
	StartedAt  time.Time      `json:"started_at,omitempty"`
	Code       string         `json:"code,omitempty"`
	ActualIP   string         `json:"actual_ip,omitempty"`
	Country    string         `json:"country,omitempty"`
	IPType     string         `json:"ip_type,omitempty"`
	LatencyMS  float64        `json:"latency_ms,omitempty"`
	CheckedAt  time.Time      `json:"checked_at"`
	ProfileSHA string         `json:"profile_sha256"`
	Stale      bool           `json:"stale"`
	Priority   bool           `json:"priority,omitempty"`
}

type CheckRun struct {
	finished    chan struct{}
	mu          sync.Mutex
	ID          string        `json:"id"`
	Status      string        `json:"status"`
	Kind        string        `json:"kind"`
	Items       []CheckResult `json:"items"`
	Done        int           `json:"done"`
	Total       int           `json:"total"`
	Parallelism int           `json:"parallelism"`
	IPTypeCheck bool          `json:"ip_type_check"`
	// poolGen records which pool generation this batch covers, so a completed
	// full check marks the generation done without another round.
	poolGen string
	cancel  context.CancelFunc
	ctx     context.Context
	wake    chan struct{}
	nodes   map[string]Node
}

func (j *CheckRun) view() any {
	j.mu.Lock()
	defer j.mu.Unlock()
	return struct {
		ID          string        `json:"id"`
		Status      string        `json:"status"`
		Kind        string        `json:"kind"`
		Items       []CheckResult `json:"items"`
		Done        int           `json:"done"`
		Total       int           `json:"total"`
		Parallelism int           `json:"parallelism"`
		IPTypeCheck bool          `json:"ip_type_check"`
	}{j.ID, j.Status, j.Kind, append([]CheckResult(nil), j.Items...), j.Done, j.Total, j.Parallelism, j.IPTypeCheck}
}

func checkFailure(err error, ctx context.Context) string {
	if ctx.Err() != nil {
		if ctx.Err() == context.Canceled {
			return "cancelled"
		}
		return "check_timeout"
	}
	text := err.Error()
	switch {
	case strings.Contains(text, "tun0"):
		return "openvpn_timeout"
	case strings.Contains(text, "openvpn"):
		return "openvpn_failed"
	case strings.Contains(text, "画像"):
		return "evidence_unavailable"
	case strings.Contains(text, "不在允许列表"):
		return "policy_type_mismatch"
	case strings.Contains(text, "出口") && strings.Contains(text, "不一致"):
		return "exit_ip_mismatch"
	case strings.Contains(text, "namespace") || strings.Contains(text, "netns"):
		return "probe_environment_failed"
	case strings.Contains(text, "写配置失败") || strings.Contains(text, "写凭据失败") ||
		strings.Contains(text, "建运行目录失败") || strings.Contains(text, "no such file"):
		return "probe_environment_failed"
	default:
		return "https_unreachable"
	}
}

// A candidate check may skip the optional type profile to avoid bulk provider
// calls. Production connections never carry this context value.
type checkIPTypeContextKey struct{}

func checkIPTypeFromContext(ctx context.Context) bool {
	if value, ok := ctx.Value(checkIPTypeContextKey{}).(bool); ok {
		return value
	}
	return true
}

// Each worker reserves a distinct diagnostic Slot outside user capacity. The
// job uses the saved operator limit and shares its dial budget with production recovery.
func (c *Console) probeCandidate(parent context.Context, node Node, slot int, progress func(string)) CheckResult {
	result := CheckResult{Protocol: "openvpn", Host: node.HostName, Status: "failed", IPType: "unknown", ProfileSHA: profileDigest(node.Config)}
	checkIPType := checkIPTypeFromContext(parent)
	if slot > diagnosticFirstSlot || slot < -networkLinkCount {
		result.Code = "probe_environment_failed"
		return result
	}
	leaseCtx, cancelLease := context.WithCancelCause(parent)
	defer cancelLease(nil)
	lease := c.m.reserveDiagnostic(node, cancelLease)
	if lease == nil {
		result.Status, result.Code = "deferred", "candidate_in_use"
		c.m.mu.RLock()
		full := len(c.m.tunnels)+len(c.m.diagnostics) >= networkLinkCount
		c.m.mu.RUnlock()
		if full {
			result.Status, result.Code = "failed", "network_capacity_exhausted"
		}
		return result
	}
	// Registered before resource defers: production cannot claim the same host
	// until the temporary OpenVPN, namespace, SOCKS and files are all gone.
	defer c.m.releaseDiagnostic(node.HostName, lease)
	slot = lease.slot
	ctx, cancel := context.WithTimeout(leaseCtx, 120*time.Second)
	defer cancel()
	progress("precheck")
	if err := preflightCandidate(ctx, node); err != nil {
		result.Code = "tcp_unreachable"
		if ctx.Err() != nil {
			result.Code = checkFailure(err, ctx)
			if result.Code == "cancelled" {
				result.Status = "cancelled"
			}
		}
		if context.Cause(leaseCtx) == errDiagnosticPreempted {
			result.Status, result.Code = "deferred", "exit_recovery_priority"
		}
		return result
	}
	dir, err := os.MkdirTemp(paths.RunDir(c.m.workDir), ".connectivity-")
	if err != nil {
		result.Code = "probe_environment_failed"
		return result
	}
	defer os.RemoveAll(dir)
	cred, err := newSocksCred()
	if err != nil {
		result.Code = "probe_environment_failed"
		return result
	}
	port, err := freeRandomPort(map[int]bool{})
	if err != nil {
		result.Code = "probe_environment_failed"
		return result
	}
	policy := SlotPolicy{AllowedIPTypes: []string{"strict_residential", "unknown"}}
	index := lease.networkIndex
	manifest, _ := json.Marshal(diagnosticResource{Config: node.Config, Slot: slot, NetworkIndex: index, Node: node})
	if err := atomicConfigWrite(paths.Diagnostic(dir), manifest); err != nil {
		result.Code = "probe_environment_failed"
		return result
	}
	t := &Tunnel{networkIndex: &index, Slot: slot, Port: port, Node: node, Status: "starting", Cred: cred, Policy: &policy, workCtx: ctx, cancelWork: cancel, onProbePhase: progress, skipEgressProfile: !checkIPType}
	helper := NewManager(1, dir)
	helper.connectBudget = c.m.connectBudget
	helper.evidenceDir = c.m.evidenceDir
	helper.shareIPTypeCache(c.m)
	helper.tunnels[t.Slot] = t
	defer func() { progress("cleanup"); t.stop(); _ = os.RemoveAll(filepath.Join("/etc/netns", t.nsName())) }()
	if err := helper.tryNode(t); err != nil {
		result.Code = checkFailure(err, ctx)
		if result.Code == "cancelled" {
			result.Status = "cancelled"
		}
		if context.Cause(leaseCtx) == errDiagnosticPreempted {
			result.Status, result.Code = "deferred", "exit_recovery_priority"
		}
		return result
	}
	// tryNode independently matches two namespace echoes with two SOCKS echoes
	// and rejects the host IP. Its accepted country may still be unknown.
	t.mu.Lock()
	result.ActualIP = t.ExitIP
	result.LatencyMS = t.LatencyMS
	if t.egress != nil {
		public := *t.egress
		public.Raw = nil
		result.Egress = &public
		if verifiedProfileCountry(*t.egress) != "" {
			result.Country = stringValue(t.egress.Country)
		}
		result.IPType = t.egress.IPType
	}
	t.mu.Unlock()
	result.Status = "passed"
	return result
}

func (c *Console) checkExit(ctx context.Context, slot int, expectedHost string, progress func(string)) CheckResult {
	c.m.mu.RLock()
	t := c.m.tunnels[slot]
	c.m.mu.RUnlock()
	result := CheckResult{Slot: slot, Status: "failed", Code: "exit_not_ready", IPType: "unknown"}
	if t == nil {
		return result
	}
	progress("waiting_exit")
	for !t.opMu.TryLock() {
		select {
		case <-ctx.Done():
			result.Status = "cancelled"
			result.Code = "cancelled"
			return result
		case <-time.After(20 * time.Millisecond):
		}
	}
	defer t.opMu.Unlock()
	v := t.snapshot()
	result.Host = v.Node.HostName
	result.ProfileSHA = profileDigest(v.Node.Config)
	if expectedHost != "" && v.Node.HostName != expectedHost {
		result.Host, result.Status, result.Code = expectedHost, "deferred", "candidate_changed"
		return result
	}
	if v.Status != "up" {
		return result
	}
	probeCtx, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()
	progress("verify_exit")
	ip, latency, err := t.probeSocksWithContext(probeCtx)
	if err != nil {
		result.Code = checkFailure(err, probeCtx)
		if result.Code == "cancelled" {
			result.Status = "cancelled"
		}
		return result
	}
	if ip == hostPublicIP() || ip != v.ExitIP {
		t.blockForPolicy("检测到实际出口变化，保持阻断")
		result.Code = "exit_ip_mismatch"
		return result
	}
	if err := t.policySnapshot().checkLatency(latency); err != nil {
		result.Code = "policy_latency_exceeded"
		return result
	}
	result.Status, result.Code, result.ActualIP, result.LatencyMS = "passed", "", ip, latency
	if v.Egress != nil {
		if verifiedProfileCountry(*v.Egress) != "" {
			result.Country = stringValue(v.Egress.Country)
		}
		result.IPType = v.Egress.IPType
	}
	return result
}
