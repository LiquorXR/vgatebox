package main

import (
	"context"
	"encoding/json"
	"fanout/internal/paths"
	"log/slog"
	"net/http"
	"os"
	"time"
)

// startTime 在包初始化时记下进程启动时间，只用于 /healthz 的 uptime 计算。
var startTime = time.Now()

// requestIDHeader 是请求 id 的出站响应头名。
const requestIDHeader = "X-Request-ID"

// requestIDKey 是 request id 在 context 里 key。未导出：只在本包内传递。
type requestIDKey struct{}

// requestIDOf 取出当前请求的 id，没有则返回空串（直接调 handler 的单测走这条路）。
func requestIDOf(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// initStructuredLogging 把 slog 默认换成 JSON（stderr，Info 级）。
// 原有 log.* 调用保持不动：迁移存量日志不在本步范围。
func initStructuredLogging() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))
}

// withRequestID 给每个请求发一个随机 id：进 context、出 X-Request-ID 响应头，
// 并记一行结构化访问日志。只记 Method + Path + 状态 + 耗时，
// 永不记 query（可能含订阅 token）、body（可能含口令/密码）与请求头。
// 放 StripBasePath 外侧：withRequestID 包在最外层，日志里是带访问路径前缀的原路径
// （仅本地 stderr，不含 query/body/头）。
func withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := randomHex(8)
		w.Header().Set(requestIDHeader, id)
		started := time.Now()
		sw := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(sw, r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id)))
		status := sw.status
		if status == 0 {
			status = http.StatusOK
		}
		slog.Info("http",
			"request_id", id,
			"method", r.Method,
			"path", r.URL.Path,
			"status", status,
			"duration_ms", time.Since(started).Milliseconds(),
		)
	})
}

// handleHealthz 是免登录探活：GET /healthz 返回版本、uptime、Slot 数、候选池龄与面板状态。
// 只含计数与版本等非敏感字段：无 token、无口令、无凭证、无节点主机名。
// 在访问路径之后（与 /sub 同层）：basepath 为空直接 /healthz；否则 <base>/healthz。
func handleHealthz(m *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		now := time.Now().UTC()
		up, total := 0, 0
		for _, t := range m.Tunnels() {
			total++
			// Status 的写方都在 t.mu/changeMu 下，用 snapshot 读（同 export.go/exits.go），
			// 裸读 live Tunnel 会与健康检查 goroutine 竞争，-race 下必报。
			if t.snapshot().Status == "up" {
				up++
			}
		}
		nodes, fetched := m.Nodes()
		pool := map[string]any{
			"candidates": len(nodes),
			"read_at":    fetched.UTC(),
		}
		if gen, ok := validatedPoolGeneratedAt(); ok {
			pool["generated_at"] = gen
			pool["age_seconds"] = int64(now.Sub(gen).Seconds())
		}
		kind := ""
		available := false
		if p, err := openPanel(); err == nil {
			available, kind = true, p.Kind()
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"status":         "ok",
			"version":        version,
			"uptime_seconds": int64(time.Since(startTime).Seconds()),
			"slots":          map[string]any{"total": total, "up": up},
			"pool":           pool,
			"panel":          map[string]any{"available": available, "kind": kind},
		})
	}
}

// validatedPoolGeneratedAt 解析共享候选池的生成时间，不存在或非法时返回 false。
// 与 apiConsole 的读法一致（同文件、同 env），只读不写。
func validatedPoolGeneratedAt() (time.Time, bool) {
	blob, err := os.ReadFile(paths.PoolFile())
	if err != nil {
		return time.Time{}, false
	}
	var pool struct {
		GeneratedAt string `json:"generated_at"`
	}
	if err := json.Unmarshal(blob, &pool); err != nil || pool.GeneratedAt == "" {
		return time.Time{}, false
	}
	gen, err := time.Parse(time.RFC3339, pool.GeneratedAt)
	if err != nil {
		return time.Time{}, false
	}
	return gen, true
}
