package main

// api_slots.go：Slot/节点/任务类 API（nodes/stop/swap/regions/cred/settings/exits/provision/jobs）。
import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
)

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// maxNodesPageLimit 封顶单页条数，避免一次拉全量大池拖慢界面。
// 不带 limit 时走旧行为：字段与顺序完全一致，不多不少。
const maxNodesPageLimit = 1000

func apiNodes(m *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		nodes, fetched := m.Nodes()
		total := len(nodes)
		// 默认跟挑节点的口径一致：开了"只用家宽"就不列 VPN Gate 自营机房节点。
		// 出口画像是否住宅由 Slot 策略判断，不能拿 Residential 代替来源标签。
		// 带 all=1 能看到完整列表，用来确认过滤掉了多少。
		if residentialOnly() && r.URL.Query().Get("all") != "1" {
			kept := make([]Node, 0, len(nodes))
			for _, n := range nodes {
				if n.SourceResidential {
					kept = append(kept, n)
				}
			}
			nodes = kept
		}
		shown := len(nodes)
		view := map[string]any{
			"nodes":            nodes,
			"fetched":          fetched,
			"total":            total,
			"available":        shown,
			"residential_only": residentialOnly(),
		}
		// 分页只在显式带 limit 时生效，切的是过滤后的列表；
		// total/available 仍是全量口径，调用方用 next_offset 翻页。
		if raw := r.URL.Query().Get("limit"); raw != "" {
			limit, err := strconv.Atoi(raw)
			if err != nil || limit < 1 || limit > maxNodesPageLimit {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "limit 参数无效"})
				return
			}
			offset := 0
			if oraw := r.URL.Query().Get("offset"); oraw != "" {
				var oerr error
				offset, oerr = strconv.Atoi(oraw)
				if oerr != nil || offset < 0 {
					writeJSON(w, http.StatusBadRequest, map[string]string{"error": "offset 参数无效"})
					return
				}
			}
			if offset > len(nodes) {
				offset = len(nodes)
			}
			end := offset + limit
			if end > len(nodes) {
				end = len(nodes)
			}
			view["nodes"] = nodes[offset:end]
			view["limit"] = limit
			view["offset"] = offset
			var next any
			if end < len(nodes) {
				next = end
			}
			view["next_offset"] = next
		}
		writeJSON(w, http.StatusOK, view)
	}
}

func apiStop(m *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		slot, err := strconv.Atoi(r.URL.Query().Get("slot"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "slot 参数无效"})
			return
		}
		if err := m.Stop(slot); err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"ok": "已停止"})
	}
}

// apiSwap 就地把一个出口换到别的节点，端口不变。
func apiSwap(m *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		slot, err := strconv.Atoi(r.URL.Query().Get("slot"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "slot 参数无效"})
			return
		}
		if err := m.Swap(slot); err != nil {
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		if m.hasPendingSwap(slot) {
			writeJSON(w, http.StatusOK, map[string]string{"ok": "已排队，当前连接完成后执行"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"ok": "正在换节点"})
	}
}

// apiRegions 是旧新建向导的兼容接口；新控制台读取 /api/console 的 regions 字段。
func apiRegions(m *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, m.Regions())
	}
}

// apiCred 改一个出口的 SOCKS5 用户名口令。两个参数都留空表示随机重置。
func apiCred(m *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Slot int    `json:"slot"`
			User string `json:"user"`
			Pass string `json:"pass"`
		}
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32768))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&in); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式无效"})
			return
		}
		if err := dec.Decode(&struct{}{}); err != io.EOF {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求格式无效"})
			return
		}
		if in.Slot <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "slot 参数无效"})
			return
		}
		cred, err := m.SetCred(in.Slot, SocksCred{
			User: strings.TrimSpace(in.User),
			Pass: strings.TrimSpace(in.Pass),
		})
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{
			"user": cred.User,
			"pass": cred.Pass,
		})
	}
}

// apiSettings 管理界面自身的设置：改密码 / 改路径 / 改端口 / 改本地监听。
// GET 返回当前值（不含明文口令）；POST 先验证完整请求，再事务提交所有字段。
func apiSettings(auth *Auth, srv *webServer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			var in managementSettingsRequest
			dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32768))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&in); err != nil {
				writeJSON(w, 400, map[string]string{"error": "请求格式错误"})
				return
			}
			if err := dec.Decode(&struct{}{}); err != io.EOF {
				writeJSON(w, 400, map[string]string{"error": "请求格式错误"})
				return
			}
			newPassword := ""
			if in.Password != nil {
				newPassword = strings.TrimSpace(*in.Password)
			}
			if newPassword != "" {
				if in.CurrentPassword == nil || strings.TrimSpace(*in.CurrentPassword) == "" {
					writeJSON(w, 400, map[string]string{"error": "修改密码需要输入当前密码"})
					return
				}
				if !auth.check(*in.CurrentPassword) {
					writeJSON(w, 400, map[string]string{"error": "当前密码不正确"})
					return
				}
			}
			if err := applyManagementSettings(auth, srv, in); err != nil {
				writeJSON(w, 400, map[string]string{"error": err.Error()})
				return
			}
			if newPassword != "" {
				keep := ""
				if c, err := r.Cookie(sessionCookie); err == nil {
					keep = c.Value
				}
				auth.invalidateOtherSessions(keep)
			}
		}

		cfg := getWebSettings()
		listen := cfg.ListenAddr
		if listen == "" {
			listen = "0.0.0.0"
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"base_path":        currentBasePath(),
			"port":             cfg.Port,
			"listen_addr":      listen,
			"has_password":     true,
			"residential_only": cfg.residentialOnly(),
			"version":          version,
		})
	}
}

// apiExits 返回主界面需要的一切：出口以及挂在它上面的入站。
func apiExits(m *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, m.ExitsOf())
	}
}

// apiProvision 接收"开 N 个某地区的出口"这个意图，返回作业 id 供轮询。
func apiProvision(m *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		count, err := strconv.Atoi(q.Get("count"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "count 参数无效"})
			return
		}
		tpl := 0
		if s := q.Get("template"); s != "" {
			if tpl, err = strconv.Atoi(s); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "template 参数无效"})
				return
			}
		}
		var policy *SlotPolicy
		if r.ContentLength != 0 {
			p, err := decodeSlotPolicy(w, r)
			if err != nil {
				writeJSON(w, 400, map[string]string{"error": "无效策略 JSON"})
				return
			}
			policy = &p
		}
		job, err := m.Provision(ProvisionRequest{
			Region: q.Get("region"), Count: count, TemplateID: tpl,
			EveryRegion: q.Get("every") == "1",
			Policy:      policy,
		})
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"job": job.ID()})
	}
}

func apiJobs(m *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, m.jobs.Views())
	}
}

func apiJobDismiss(m *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		m.jobs.Dismiss(r.URL.Query().Get("id"))
		writeJSON(w, http.StatusOK, map[string]string{"ok": "已关闭"})
	}
}
