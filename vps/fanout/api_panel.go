package main

// api_panel.go：面板/入站/客户端类 API（xui/inbound/client）。
import (
	"net/http"
	"strconv"
	"strings"
)

// apiXUIInbounds 列出内置 Xray 中已有的入站及其绑定状态。
func apiXUIInbounds(m *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		list, err := cachedInbounds(liveHosts(m))
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, list)
	}
}

// liveHosts 返回当前有连通隧道的节点标识集合。
func liveHosts(m *Manager) map[string]bool {
	live := map[string]bool{}
	for _, t := range m.Tunnels() {
		v := t.snapshot()
		if v.Status == "up" {
			live[sanitizeTag(v.Node.HostName)] = true
			live[slotOutboundTag(v.Slot)] = true
		}
	}
	return live
}

// apiXUIBind 把某个入站绑定到某条隧道，slot=0 表示解绑。
func apiXUIBind(m *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tag := r.URL.Query().Get("tag")
		if tag == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "缺少 tag 参数"})
			return
		}
		host := r.URL.Query().Get("host")
		x, err := openPanel()
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		if raw := r.URL.Query().Get("slot"); raw != "" {
			slot, parseErr := strconv.Atoi(raw)
			n, native := x.(*Native)
			if parseErr != nil || slot < 0 || !native {
				writeJSON(w, 400, map[string]string{"error": "出口编号无效"})
				return
			}
			err = n.BindSlot(tag, slot, m.Tunnels())
		} else {
			err = x.Bind(tag, host, m.Tunnels())
		}
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		invalidateInbounds()
		writeJSON(w, http.StatusOK, map[string]string{"ok": "已更新"})
	}
}

// apiXUIClone 以某个入站为模板，为所有已连通的隧道各复制一个入站并绑好出口。
func apiXUIClone(m *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.Atoi(r.URL.Query().Get("id"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id 参数无效"})
			return
		}

		tunnels := m.Tunnels()
		// 用节点主机名而非槽位号：槽位在重启后会重排，指代会错位
		var hosts []string
		if raw := r.URL.Query().Get("hosts"); raw != "" {
			for _, part := range strings.Split(raw, ",") {
				if h := strings.TrimSpace(part); h != "" {
					hosts = append(hosts, h)
				}
			}
		} else {
			for _, t := range tunnels {
				if t.Status == "up" {
					hosts = append(hosts, t.Node.HostName)
				}
			}
		}
		if len(hosts) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "没有可用的隧道"})
			return
		}

		x, err := openPanel()
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		ports, err := x.CloneToTunnels(id, hosts, tunnels)
		invalidateInbounds()
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error(), "created": ports})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"created": ports})
	}
}

// apiXUIDetail 返回某个入站的详情，含客户端与可直接复制的分享链接。
func apiXUIDetail(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.URL.Query().Get("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id 参数无效"})
		return
	}
	x, err := openPanel()
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	host := r.URL.Query().Get("host")
	if host == "" {
		host = publicHost(r)
	}
	detail, err := x.InboundDetail(id, host)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

// publicHost 决定分享链接里的连接地址。母机公网 IPv4 才是客户端真正能连上
// 的地址，所以优先用它；探测不到（比如纯内网）再退回访问 fanout 时用的主机名。
func publicHost(r *http.Request) string {
	if ip := hostPublicIP(); ip != "" {
		return ip
	}
	host := publicHostWithoutPort(r)
	if host == "" || host == "127.0.0.1" || host == "localhost" {
		return "<服务器IP>"
	}
	return host
}

// apiXUILinks 批量导出多个入站的分享链接。
func apiXUILinks(w http.ResponseWriter, r *http.Request) {
	x, err := openPanel()
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}

	var ids []int
	if raw := r.URL.Query().Get("ids"); raw != "" {
		for _, part := range strings.Split(raw, ",") {
			if n, err := strconv.Atoi(strings.TrimSpace(part)); err == nil {
				ids = append(ids, n)
			}
		}
	} else {
		list, err := x.Inbounds(nil)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		for _, ib := range list {
			ids = append(ids, ib.ID)
		}
	}
	if len(ids) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "没有可导出的入站"})
		return
	}

	host := r.URL.Query().Get("host")
	if host == "" {
		host = publicHost(r)
	}
	links, err := x.InboundLinks(ids, host)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error(), "links": links})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"links": links})
}

// apiXUIRotateEncryption 重新生成一个 VLESS 入站的协议内加密参数。
func apiXUIRotateEncryption(m *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.Atoi(r.URL.Query().Get("id"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id 参数无效"})
			return
		}
		p, err := openPanel()
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		n, ok := p.(*Native)
		if !ok {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "仅支持内置 Xray"})
			return
		}
		enc, err := n.RotateVLESSEncryption(id, m.Tunnels())
		invalidateInbounds()
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{
			"client_encryption": enc.ClientEncryption,
			"key_type":          enc.KeyType,
		})
	}
}

// apiXUISetEncryption 打开或关闭一个 VLESS 入站的协议内加密层。
func apiXUISetEncryption(m *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.Atoi(r.URL.Query().Get("id"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id 参数无效"})
			return
		}
		enabled := r.URL.Query().Get("enabled") == "1"
		keyType := r.URL.Query().Get("key_type")
		p, err := openPanel()
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		n, ok := p.(*Native)
		if !ok {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "仅支持内置 Xray"})
			return
		}
		enc, err := n.SetVLESSEncryption(id, enabled, keyType, m.Tunnels())
		invalidateInbounds()
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if !enabled || enc == nil {
			writeJSON(w, http.StatusOK, map[string]bool{"enabled": false})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"enabled":           true,
			"client_encryption": enc.ClientEncryption,
			"key_type":          enc.KeyType,
		})
	}
}

// apiXUIDelete 删除入站。停掉出口后它的入站会留下来，用户需要一个清理入口。
func apiXUIDelete(m *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var ids []int
		for _, part := range strings.Split(r.URL.Query().Get("ids"), ",") {
			if n, err := strconv.Atoi(strings.TrimSpace(part)); err == nil {
				ids = append(ids, n)
			}
		}
		if len(ids) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "没有指定要删除的入站"})
			return
		}
		x, err := openPanel()
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		err = x.DeleteInbounds(ids, m.Tunnels())
		invalidateInbounds()
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]int{"deleted": len(ids)})
	}
}

// apiInboundUpdate 修改内置 Xray 入站的端口、备注与启停。
func apiInboundUpdate(m *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, err := openPanel()
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		q := r.URL.Query()
		id, err := strconv.Atoi(q.Get("id"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id 参数无效"})
			return
		}

		// 只有真正传了的参数才改，没传的保持原样
		var patch InboundPatch
		if mode := q.Get("name_mode"); mode != "" {
			patch.NameMode = &mode
		}
		if v := q.Get("port"); v != "" {
			port, err := strconv.Atoi(v)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "端口无效"})
				return
			}
			patch.Port = &port
		}
		if q.Has("remark") {
			remark := q.Get("remark")
			patch.Remark = &remark
		}
		if v := q.Get("enable"); v != "" {
			enable := v == "1"
			patch.Enable = &enable
		}

		err = p.UpdateInbound(id, patch, m.Tunnels())
		invalidateInbounds()
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"ok": "已保存"})
	}
}

// clientAction 把三个客户端操作的公共部分收拢：解析 id/email 再调后端。
func clientAction(m *Manager, what string,
	do func(p Panel, id int, email string, tunnels []*Tunnel) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, err := openPanel()
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		id, err := strconv.Atoi(r.URL.Query().Get("id"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id 参数无效"})
			return
		}
		err = do(p, id, r.URL.Query().Get("email"), m.Tunnels())
		invalidateInbounds()
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"ok": what})
	}
}

func apiClientAdd(m *Manager) http.HandlerFunc {
	return clientAction(m, "已添加", func(p Panel, id int, email string, t []*Tunnel) error {
		return p.AddClient(id, email, t)
	})
}

func apiClientDelete(m *Manager) http.HandlerFunc {
	return clientAction(m, "已删除", func(p Panel, id int, email string, t []*Tunnel) error {
		return p.DeleteClient(id, email, t)
	})
}

func apiClientReset(m *Manager) http.HandlerFunc {
	return clientAction(m, "已重置", func(p Panel, id int, email string, t []*Tunnel) error {
		return p.ResetClient(id, email, t)
	})
}

func apiInboundCreate(m *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, err := openPanel()
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}

		q := r.URL.Query()
		port, _ := strconv.Atoi(q.Get("port"))
		kcpMTU, _ := strconv.Atoi(q.Get("kcp_mtu"))
		kcpTTI, _ := strconv.Atoi(q.Get("kcp_tti"))
		kcpUplink, _ := strconv.Atoi(q.Get("kcp_uplink"))
		kcpDownlink, _ := strconv.Atoi(q.Get("kcp_downlink"))
		slot := 0
		if raw := q.Get("slot"); raw != "" {
			var parseErr error
			slot, parseErr = strconv.Atoi(raw)
			_, native := p.(*Native)
			if parseErr != nil || slot < 0 || !native {
				writeJSON(w, 400, map[string]string{"error": "出口编号无效"})
				return
			}
		}
		ib, err := p.CreateInbound(NewInboundSpec{
			BoundSlot: slot,
			Protocol:  q.Get("protocol"),
			Network:   q.Get("network"),
			Port:      port,
			Remark:    q.Get("remark"),
			Path:      q.Get("path"),
			Host:      q.Get("host"),
			Security:  q.Get("security"),
			Vision:    q.Get("vision") == "1",

			ServerName: q.Get("sni"),
			CertFile:   q.Get("cert"),
			KeyFile:    q.Get("key"),

			Dest:        q.Get("dest"),
			ServerNames: q.Get("server_names"),
			ShortID:     q.Get("sid"),
			Fingerprint: q.Get("fp"),

			KCPMTU:      kcpMTU,
			KCPTTI:      kcpTTI,
			KCPUplink:   kcpUplink,
			KCPDownlink: kcpDownlink,

			VLESSEncryption:     q.Get("vless_encryption") == "1",
			VLESSEncryptionType: q.Get("vless_encryption_type"),
		}, m.Tunnels())
		invalidateInbounds()
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"id":       ib.ID,
			"port":     ib.Port,
			"protocol": ib.Protocol,
			"remark":   ib.Remark,
			"network":  ib.Network,
			"security": ib.Security,
		})
	}
}
