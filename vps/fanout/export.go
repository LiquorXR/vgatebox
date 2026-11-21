package main

import (
	"fmt"
	"net/http"
)

func slotOutboundTag(slot int) string { return fmt.Sprintf("slot-%02d", slot) }

func slotOutbounds(tunnels []*Tunnel) []any {
	// Xray's first outbound is the default for requests without a matching rule.
	outs := []any{map[string]any{"tag": "block", "protocol": "blackhole"}}
	for _, t := range tunnels {
		if t.Slot < 1 {
			continue
		}
		out := map[string]any{"tag": slotOutboundTag(t.Slot), "protocol": "blackhole"}
		// 仅 serving 状态（up/degraded）在 Xray 层保持 socks 出站；
		// starting/failed/stopped/waiting 等在 Xray 层即阻断为 blackhole，
		// 与 Go 侧 acceptsSocksLocked() + Kill Switch 形成纵深 Fail-Closed。
		// 健康抖动等无变 apply 经 native_transaction.go sameConfig 快路径
		// 收敛为 no-op，不触发全局重启（见 ADR-0006）。
		if servingStatus(t.snapshot().Status) {
			out["protocol"] = "socks"
			out["settings"] = map[string]any{"servers": []any{socksServerJSON(t)}}
		}
		outs = append(outs, out)
	}
	return outs
}

// Export a standard fragment for manual integration without touching panel data.
func apiExportXray(m *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		tunnels := m.exportTunnels()
		rules := []any{}
		for _, t := range tunnels {
			if t.Slot > 0 {
				rules = append(rules, map[string]any{"type": "field",
					"inboundTag":  []string{"inbound-" + slotOutboundTag(t.Slot)},
					"outboundTag": slotOutboundTag(t.Slot)})
			}
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Disposition", `attachment; filename="vgatebox-xray.json"`)
		writeJSON(w, http.StatusOK, map[string]any{"outbounds": slotOutbounds(tunnels),
			"routing": map[string]any{"rules": rules}})
	}
}
