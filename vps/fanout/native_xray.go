package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// xrayCandidates 是内置模式查找 xray 二进制的位置，按优先级排列。
// 优先用 fanout 自己装的那份，避免和系统里的其他 xray 抢版本。
func xrayCandidates(workDir string) []string {
	return []string{
		filepath.Join(workDir, "bin", "xray"),
		"/usr/local/bin/xray",
		"/usr/bin/xray",
	}
}

// findXray 定位可执行的 xray。
func findXray(workDir string) (string, error) {
	for _, p := range xrayCandidates(workDir) {
		if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Mode()&0111 != 0 {
			return p, nil
		}
	}
	if p, err := exec.LookPath("xray"); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("找不到 xray 可执行文件，装一份到 %s 或 /usr/local/bin/xray",
		filepath.Join(workDir, "bin", "xray"))
}

// buildXrayConfig 由入站列表和当前隧道生成完整的 Xray 运行配置。
//
// Every slot keeps its outbound identity when the backing node changes.
// Unbound, unavailable and stale bindings are blocked, never sent via the host.
func buildXrayConfig(inbounds []*nativeInbound, tunnels []*Tunnel) map[string]any {
	bound := map[string]string{}
	slots := map[int]bool{}
	for _, t := range tunnels {
		if t.Slot > 0 {
			slots[t.Slot] = true
			if host := t.snapshot().Node.HostName; host != "" {
				bound[sanitizeTag(host)] = slotOutboundTag(t.Slot)
			}
		}
	}

	bySlot := map[int]*Tunnel{}
	for _, t := range tunnels {
		bySlot[t.Slot] = t
	}
	ins := make([]any, 0, len(inbounds))
	for _, ib := range inbounds {
		if !ib.Enable {
			continue
		}
		if ib.SocksSlot > 0 {
			copy := *ib
			if t := bySlot[ib.SocksSlot]; t != nil {
				c := t.credential()
				copy.Clients = []nativeClient{{Email: c.User, Password: c.Pass, Enable: true}}
			}
			ib = &copy
		}
		ins = append(ins, nativeInboundJSON(ib))
	}

	outs := slotOutbounds(tunnels)
	for _, ib := range inbounds {
		if thirdPartyBound(ib) && ib.Enable {
			p := ib.ThirdParty
			outs = append(outs, map[string]any{"tag": thirdPartyTag(ib.ID), "protocol": "socks", "settings": map[string]any{"servers": []any{map[string]any{"address": p.Host, "port": p.Port, "users": []any{map[string]any{"user": p.Username, "pass": p.Password}}}}}})
		}
	}
	for _, ib := range inbounds {
		if ib.Enable && ib.HostDirect && ib.BoundSlot == 0 && ib.BoundTo == "" && ib.SocksSlot == 0 {
			outs = append(outs, map[string]any{"tag": "host-direct", "protocol": "freedom"})
			break
		}
	}

	rules := []any{}
	for _, ib := range inbounds {
		if !ib.Enable {
			continue
		}
		tag := bound[ib.BoundTo]
		if ib.BoundSlot > 0 {
			tag = ""
			if slots[ib.BoundSlot] {
				tag = slotOutboundTag(ib.BoundSlot)
			}
		}
		if ib.HostDirect && ib.BoundSlot == 0 && ib.BoundTo == "" && ib.SocksSlot == 0 {
			tag = "host-direct"
		}
		if ib.ThirdParty != nil {
			tag = ""
			if thirdPartyBound(ib) {
				tag = thirdPartyTag(ib.ID)
			}
		}
		if tag == "" {
			tag = "block"
		}
		rules = append(rules, map[string]any{
			"type":        "field",
			"inboundTag":  []any{ib.tag()},
			"outboundTag": tag,
		})
	}

	return map[string]any{
		"log":       map[string]any{"loglevel": "warning"},
		"inbounds":  ins,
		"outbounds": outs,
		"routing": map[string]any{
			"domainStrategy": "AsIs",
			"rules":          rules,
		},
	}
}

// nativeInboundJSON 把一个入站转成 Xray 的 inbound 配置。
func nativeInboundJSON(ib *nativeInbound) map[string]any {
	settings := map[string]any{}
	switch ib.Protocol {
	case "hysteria":
		clients := []any{}
		for _, c := range ib.Clients {
			if !c.Enable {
				continue
			}
			auth := c.Password
			if auth == "" && ib.Hysteria != nil {
				auth = ib.Hysteria.Auth
			}
			clients = append(clients, map[string]any{"auth": auth, "email": c.Email})
		}
		settings["version"] = 2
		// Hysteria2 服务端的用户字段是 clients；新版本也接受 users，
		// 但 clients 对旧版本同样有效，统一输出这一种。
		settings["clients"] = clients
	case "socks":
		accounts := []any{}
		for _, c := range ib.Clients {
			if c.Enable {
				accounts = append(accounts, map[string]any{"user": c.Email, "pass": c.Password})
			}
		}
		settings = map[string]any{"auth": "password", "accounts": accounts, "udp": true}
	default: // vless
		clients := make([]any, 0, len(ib.Clients))
		for _, c := range ib.Clients {
			if c.Enable {
				clients = append(clients, map[string]any{"id": c.ID, "email": c.Email, "flow": c.Flow})
			}
		}
		settings["clients"] = clients
		decryption := "none"
		if ib.VLESSEncryption != nil && ib.VLESSEncryption.Enabled && ib.VLESSEncryption.ServerDecryption != "" {
			decryption = ib.VLESSEncryption.ServerDecryption
		}
		settings["decryption"] = decryption
	}

	return map[string]any{
		"tag":            ib.tag(),
		"listen":         "0.0.0.0",
		"port":           inboundPortValue(ib),
		"protocol":       ib.Protocol,
		"settings":       settings,
		"streamSettings": streamSettingsJSON(ib),
		"sniffing":       map[string]any{"enabled": true, "destOverride": []any{"http", "tls"}},
	}
}

// inboundPortValue 对 Hysteria2 服务端端口跳跃返回范围字符串；其他情况返回单端口。
func inboundPortValue(ib *nativeInbound) any {
	if ib.netOrTCP() == "hysteria" && ib.Hysteria != nil && strings.TrimSpace(ib.Hysteria.Ports) != "" {
		return strings.TrimSpace(ib.Hysteria.Ports)
	}
	return ib.Port
}

// streamSettingsJSON 生成传输层配置：网络类型 + 安全层。
func streamSettingsJSON(ib *nativeInbound) map[string]any {
	network := ib.netOrTCP()
	stream := map[string]any{"network": network, "security": ib.securityOrNone()}

	path := ib.Path
	if path == "" {
		path = "/"
	}
	switch network {
	case "ws":
		ws := map[string]any{"path": path}
		if ib.Host != "" {
			ws["host"] = ib.Host
		}
		stream["wsSettings"] = ws
	case "httpupgrade":
		hu := map[string]any{"path": path}
		if ib.Host != "" {
			hu["host"] = ib.Host
		}
		stream["httpupgradeSettings"] = hu
	case "xhttp":
		xh := map[string]any{"path": path, "mode": "auto"}
		if ib.Host != "" {
			xh["host"] = ib.Host
		}
		stream["xhttpSettings"] = xh
	case "grpc":
		// gRPC 没有 path，用 serviceName 区分；沿用 Path 字段少一个概念
		name := strings.TrimPrefix(ib.Path, "/")
		stream["grpcSettings"] = map[string]any{"serviceName": name}
	case "mkcp":
		stream["kcpSettings"] = kcpSettingsJSON(ib.KCP)
	case "hysteria":
		stream["hysteriaSettings"] = hysteriaSettingsJSON(ib)
	}

	switch ib.securityOrNone() {
	case "tls":
		if ib.TLS != nil {
			t := map[string]any{
				"certificates": []any{map[string]any{
					"certificateFile": ib.TLS.CertFile,
					"keyFile":         ib.TLS.KeyFile,
				}},
			}
			if ib.TLS.ServerName != "" {
				t["serverName"] = ib.TLS.ServerName
			}
			// Hysteria2 跑在 QUIC 上，服务端必须显式协商 h3。
			if network == "hysteria" {
				t["alpn"] = []any{"h3"}
				t["minVersion"] = "1.3"
			}
			stream["tlsSettings"] = t
		}
	case "reality":
		if ib.Reality != nil {
			r := map[string]any{
				"dest":        ib.Reality.Dest,
				"serverNames": toAnySlice(ib.Reality.ServerNames),
				"privateKey":  ib.Reality.PrivateKey,
				"shortIds":    toAnySlice(ib.Reality.ShortIDs),
			}
			stream["realitySettings"] = r
		}
	}
	if fm := finalMaskJSON(ib); fm != nil {
		stream["finalmask"] = fm
	}
	return stream
}

func kcpSettingsJSON(k *kcpConfig) map[string]any {
	out := map[string]any{
		"mtu":              1350,
		"tti":              50,
		"uplinkCapacity":   5,
		"downlinkCapacity": 20,
		"cwndMultiplier":   1,
		"maxSendingWindow": 2097152,
	}
	if k != nil {
		if k.MTU > 0 {
			out["mtu"] = k.MTU
		}
		if k.TTI > 0 {
			out["tti"] = k.TTI
		}
		if k.Uplink > 0 {
			out["uplinkCapacity"] = k.Uplink
		}
		if k.Downlink > 0 {
			out["downlinkCapacity"] = k.Downlink
		}
	}
	return out
}

func hysteriaSettingsJSON(ib *nativeInbound) map[string]any {
	h := ib.Hysteria
	out := map[string]any{"version": 2}
	auth := ""
	if ib.Protocol == "hysteria" {
		for _, c := range ib.Clients {
			if c.Enable && c.Password != "" {
				auth = c.Password
				break
			}
		}
	}
	if auth == "" && h != nil {
		auth = h.Auth
	}
	if auth != "" {
		out["auth"] = auth
	}
	if h == nil {
		return out
	}
	if h.UDPIdleTimeout > 0 {
		out["udpIdleTimeout"] = h.UDPIdleTimeout
	}
	if h.MasqueradeType != "" {
		m := map[string]any{"type": h.MasqueradeType}
		switch h.MasqueradeType {
		case "file":
			m["dir"] = h.MasqueradeURL
		case "proxy":
			m["url"] = h.MasqueradeURL
		case "string":
			m["content"] = h.MasqueradeContent
		}
		out["masquerade"] = m
	}
	return out
}

func finalMaskJSON(ib *nativeInbound) map[string]any {
	h := ib.Hysteria
	if h == nil {
		return nil
	}
	fm := map[string]any{}
	if h.ObfsPassword != "" {
		fm["udp"] = []any{map[string]any{
			"type":     "salamander",
			"settings": map[string]any{"password": h.ObfsPassword},
		}}
	}
	qp := map[string]any{}
	if h.Up != "" || h.Down != "" {
		qp["congestion"] = "brutal"
		if h.Up != "" {
			qp["brutalUp"] = h.Up
		}
		if h.Down != "" {
			qp["brutalDown"] = h.Down
		}
	}
	if len(qp) > 0 {
		fm["quicParams"] = qp
	}
	if len(fm) == 0 {
		return nil
	}
	return fm
}

func toAnySlice(in []string) []any {
	out := make([]any, 0, len(in))
	for _, s := range in {
		out = append(out, s)
	}
	return out
}

// writeXrayConfig 把配置写到磁盘，返回配置路径。
func writeXrayConfig(dir string, cfg map[string]any) (string, error) {
	blob, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "xray.json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, blob, 0600); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		return "", err
	}
	return path, nil
}

// verifyXrayConfig 用 xray 自己的校验器检查配置。
//
// 先校验再重启：配置写坏时进程会起不来，而那时旧进程已经被杀掉，
// 所有节点链接会一起断掉。校验能把这类错误挡在重启之前。
func verifyXrayConfig(bin, cfgPath string) error {
	_, err := cmdCombined(exec.Command(bin, "run", "-test", "-c", cfgPath))
	if err != nil {
		return fmt.Errorf("Xray 配置校验失败: %w", err)
	}
	return nil
}

func trimOutput(b []byte) string {
	s := string(b)
	if len(s) > 400 {
		s = s[:400] + "..."
	}
	return s
}

// xrayProc 管理自建模式下的 xray 进程。
type xrayProc struct {
	bin  string
	dir  string
	cmd  *exec.Cmd
	logf *os.File
	done chan error
}

func (p *xrayProc) verify(path string) error { return verifyXrayConfig(p.bin, path) }

// restart 用当前配置重启 xray。配置已在调用前写好并校验过。
func (p *xrayProc) restart(cfgPath string) error {
	p.stop()

	logPath := filepath.Join(p.dir, "xray.log")
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return fmt.Errorf("打开 Xray 日志失败: %w", err)
	}

	cmd := exec.Command(p.bin, "run", "-c", cfgPath)
	cmd.Stdout = f
	cmd.Stderr = f
	// 必须从母机的网络命名空间拉起：直接 Start 有概率把 Xray 拉进某条隧道，
	// 那样入站端口只在隧道内部监听，母机上看不到，客户端全连不上
	// 而进程、配置、日志三处都显示正常（见 netnsguard.go）
	if err := cmdStart(cmd); err != nil {
		f.Close()
		return fmt.Errorf("启动 Xray 失败: %w", err)
	}
	p.cmd, p.logf = cmd, f
	p.done = make(chan error, 1)
	finished := p.done
	go func() { finished <- cmd.Wait() }()
	if err := p.writePID(cmd.Process.Pid); err != nil {
		p.stop()
		return fmt.Errorf("保存 Xray 进程标识失败: %w", err)
	}

	// 起得来但立刻退出的情况要能被发现，否则界面会显示成功而实际不通
	select {
	case <-p.done:
		p.cmd = nil
		p.done = nil
		return fmt.Errorf("Xray 启动后立刻退出，详见 %s", logPath)
	case <-time.After(400 * time.Millisecond):
		return nil
	}
}

func (p *xrayProc) stop() {
	if p.cmd != nil && p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
		if p.done != nil {
			select {
			case <-p.done:
			case <-time.After(2 * time.Second):
			}
		}
		p.cmd = nil
	}
	p.done = nil
	if p.logf != nil {
		p.logf.Close()
		p.logf = nil
	}
	_ = os.Remove(p.pidPath())
}

func (p *xrayProc) pidPath() string { return filepath.Join(p.dir, "xray.pid") }

func (p *xrayProc) writePID(pid int) error {
	return atomicConfigWrite(p.pidPath(), []byte(strconv.Itoa(pid)))
}

// reapOrphan 清掉上次遗留的 Xray。
//
// fanout 被 SIGKILL 时来不及停子进程，遗留的 Xray 仍占着入站端口，
// 下次启动会因端口冲突起不来。这里按 pidfile 精确定位，
// 并核对可执行文件确实是我们启动的那个，避免误杀同名进程。
func (p *xrayProc) reapOrphan() {
	blob, err := os.ReadFile(p.pidPath())
	if err != nil {
		return
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(blob)))
	if err != nil || pid <= 1 {
		_ = os.Remove(p.pidPath())
		return
	}
	// pid 会被系统回收给别的进程，只有确认可执行文件一致才动手
	if exe, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid)); err == nil {
		if exe == p.bin {
			if proc, err := os.FindProcess(pid); err == nil {
				_ = proc.Kill()
			}
		}
	}
	_ = os.Remove(p.pidPath())
}
