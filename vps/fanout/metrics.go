package main

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Prometheus 指标（P2-5）：只读运维视角，不含任何凭据、token 与节点身份。
// 端点挂 /api/metrics，走登录会话认证（Auth.Wrap 对 /api/* 未登录回 401），
// Cache-Control: no-store。抓取间隔建议 15s 以上。

// httpRequestsTotal 按 {path,status} 计数，key 用 \x00 连接。
var (
	httpRequestsMu    sync.Mutex
	httpRequestsTotal = map[string]uint64{}
)

// xrayRestarts 统计本实例成功拉起 Xray 的次数（apply/restoreRuntime 的 restart 成功处递增）。
var xrayRestarts atomic.Uint64

func noteXrayRestart() { xrayRestarts.Add(1) }

// metricsPathLabel 收敛 path 标签：只保留固定路由，其余记 other。
// withStatusLog 包在 StripBasePath 之外，但它在 next 返回后才计数，
// 此时 Path 已被剥掉随机访问前缀；这里再守一道：随机路径、未知路径
// 永不进 label（基数爆炸 + 变相泄密）。
func metricsPathLabel(path string) string {
	switch {
	case path == "/" || path == "/sub" || path == "/login" || path == "/healthz":
		return path
	case strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/next/") || strings.HasPrefix(path, "/assets/"):
		return path
	default:
		return "other"
	}
}

func noteHTTPRequest(path string, status int) {
	key := metricsPathLabel(path) + "\x00" + strconv.Itoa(status)
	httpRequestsMu.Lock()
	httpRequestsTotal[key]++
	httpRequestsMu.Unlock()
}

func writeMetricLine(sb *strings.Builder, name, help, typ, labels string, value int64) {
	if labels == "" {
		fmt.Fprintf(sb, "# HELP %s %s\n# TYPE %s %s\n%s %d\n", name, help, name, typ, name, value)
		return
	}
	fmt.Fprintf(sb, "# HELP %s %s\n# TYPE %s %s\n%s{%s} %d\n", name, help, name, typ, name, labels, value)
}

// apiMetrics 输出 Prometheus 文本格式。计数类读内存快照，不落盘。
func (c *Console) apiMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	up, blocked, degraded, total := 0, 0, 0, 0
	for _, t := range c.m.Tunnels() {
		total++
		switch t.snapshot().Status {
		case "up":
			up++
		case "degraded":
			degraded++
		case "blocked":
			blocked++
		}
	}
	poolAge := int64(-1)
	if gen, ok := validatedPoolGeneratedAt(); ok {
		poolAge = int64(time.Since(gen).Seconds())
	}
	checkNodes, checkPassed := int64(-1), int64(-1)
	if c.automation != nil {
		a := c.automation
		a.mu.Lock()
		checkNodes, checkPassed = int64(a.check.Nodes), int64(a.check.Passed)
		a.mu.Unlock()
	}
	var sb strings.Builder
	writeMetricLine(&sb, "vgatebox_slots_up", "Slots currently up", "gauge", "", int64(up))
	writeMetricLine(&sb, "vgatebox_slots_degraded", "Slots degraded but still serving", "gauge", "", int64(degraded))
	writeMetricLine(&sb, "vgatebox_slots_blocked", "Slots blocked for policy or evidence", "gauge", "", int64(blocked))
	writeMetricLine(&sb, "vgatebox_slots_total", "Slots known to the manager", "gauge", "", int64(total))
	writeMetricLine(&sb, "vgatebox_pool_age_seconds", "Validated pool age, -1 when unknown", "gauge", "", poolAge)
	writeMetricLine(&sb, "vgatebox_xray_restarts_total", "Xray process starts by this instance", "counter", "", int64(xrayRestarts.Load()))
	writeMetricLine(&sb, "vgatebox_check_last_total", "Nodes in the last finished check, -1 when never", "gauge", "", checkNodes)
	writeMetricLine(&sb, "vgatebox_check_last_passed", "Passed nodes in the last finished check, -1 when never", "gauge", "", checkPassed)
	fmt.Fprintf(&sb, "# HELP vgatebox_http_requests_total HTTP requests by path and status\n# TYPE vgatebox_http_requests_total counter\n")
	httpRequestsMu.Lock()
	keys := make([]string, 0, len(httpRequestsTotal))
	for k := range httpRequestsTotal {
		keys = append(keys, k)
	}
	for _, k := range keys {
		path, status, _ := strings.Cut(k, "\x00")
		fmt.Fprintf(&sb, "vgatebox_http_requests_total{path=%q,status=%q} %d\n",
			path, status, httpRequestsTotal[k])
	}
	httpRequestsMu.Unlock()
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(sb.String()))
}
