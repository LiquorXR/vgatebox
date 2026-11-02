package main

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

func decodeShareBase64(s string) ([]byte, error) {
	for _, e := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if b, err := e.DecodeString(s); err == nil {
			return b, nil
		}
	}
	return nil, fmt.Errorf("invalid_share_base64")
}

func normalizeVmessShare(link string) (string, error) {
	if !strings.HasPrefix(link, "vmess://") || strings.Contains(strings.SplitN(strings.TrimPrefix(link, "vmess://"), "#", 2)[0], "@") {
		return link, nil
	}
	b, err := decodeShareBase64(strings.SplitN(strings.TrimPrefix(link, "vmess://"), "#", 2)[0])
	if err != nil {
		return "", err
	}
	var v map[string]any
	if json.Unmarshal(b, &v) != nil {
		return "", fmt.Errorf("invalid_vmess_share")
	}
	value := func(k string) string {
		if v[k] == nil {
			return ""
		}
		return fmt.Sprint(v[k])
	}
	q := url.Values{"type": {value("net")}, "security": {"none"}, "sni": {value("sni")}, "host": {value("host")}, "path": {value("path")}, "alterId": {value("aid")}, "encryption": {value("scy")}, "fp": {value("fp")}}
	if value("tls") == "tls" {
		q.Set("security", "tls")
	}
	if value("net") == "grpc" {
		q.Set("serviceName", value("path"))
	}
	u := url.URL{Scheme: "vmess", Host: net.JoinHostPort(value("add"), value("port")), User: url.User(value("id")), RawQuery: q.Encode(), Fragment: value("ps")}
	return u.String(), nil
}

type clashUnsupportedError struct{ reason string }

func (e *clashUnsupportedError) Error() string { return e.reason }

func clashProxy(link string) (map[string]any, error) {
	link, err := normalizeVmessShare(link)
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(link)
	if err != nil || u.User == nil || u.Hostname() == "" {
		return nil, fmt.Errorf("invalid_share_link")
	}
	protoName := strings.ToLower(u.Scheme)
	if protoName == "hysteria2" || protoName == "hy2" {
		return clashHysteria2(u)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port < 1 || port > 65535 {
		return nil, fmt.Errorf("invalid_share_port")
	}
	q := u.Query()
	protocol := protoName
	switch protocol {
	case "vless", "vmess", "trojan":
	default:
		return nil, fmt.Errorf("unsupported_clash_protocol")
	}
	transport := strings.ToLower(strings.TrimSpace(q.Get("type")))
	if transport == "" {
		transport = "tcp"
	}
	p := map[string]any{"name": u.Fragment, "server": u.Hostname(), "port": port, "udp": true}
	switch protocol {
	case "vless":
		p["type"], p["uuid"] = "vless", u.User.Username()
	case "vmess":
		p["type"], p["uuid"], p["cipher"], p["alterId"] = "vmess", u.User.Username(), "auto", 0
		if a := q.Get("alterId"); a != "" {
			n, e := strconv.Atoi(a)
			if e != nil || n < 0 {
				return nil, fmt.Errorf("invalid_vmess_alter_id")
			}
			p["alterId"] = n
		}
		if cipher := q.Get("encryption"); cipher != "" {
			p["cipher"] = cipher
		}
	case "trojan":
		p["type"], p["password"] = "trojan", u.User.Username()
	}
	if u.User.Username() == "" {
		return nil, fmt.Errorf("missing_share_credential")
	}
	switch transport {
	case "tcp", "raw":
	case "ws":
		p["network"] = "ws"
		p["ws-opts"] = clashWSOpts(q, false)
	case "httpupgrade", "http-upgrade":
		p["network"] = "ws"
		p["ws-opts"] = clashWSOpts(q, true)
	case "grpc":
		p["network"] = "grpc"
		p["grpc-opts"] = map[string]any{"grpc-service-name": strings.TrimPrefix(q.Get("serviceName"), "/")}
	case "xhttp":
		if protocol != "vless" {
			return nil, &clashUnsupportedError{reason: "Mihomo only supports XHTTP with VLESS"}
		}
		p["network"] = "xhttp"
		p["xhttp-opts"] = clashXHTTPOpts(q)
	case "mkcp", "kcp":
		return nil, &clashUnsupportedError{reason: "Mihomo cannot represent the mKCP transport"}
	case "hysteria":
		return nil, &clashUnsupportedError{reason: "Mihomo cannot use the Hysteria2 transport for this protocol"}
	default:
		return nil, fmt.Errorf("unsupported_clash_transport")
	}
	security := strings.ToLower(strings.TrimSpace(q.Get("security")))
	if security == "" {
		security = "none"
		if protocol == "trojan" {
			security = "tls"
		}
	}
	if security != "none" && security != "tls" && security != "reality" {
		return nil, fmt.Errorf("unsupported_clash_security")
	}
	if protocol == "trojan" && security == "none" {
		return nil, &clashUnsupportedError{reason: "Mihomo forces TLS for Trojan"}
	}
	if security != "none" {
		p["tls"], p["skip-cert-verify"] = true, false
		key := "servername"
		if protocol == "trojan" {
			key = "sni"
		}
		if sni := q.Get("sni"); sni != "" {
			p[key] = sni
		}
		if fp := q.Get("fp"); fp != "" {
			p["client-fingerprint"] = fp
		} else if security == "reality" {
			p["client-fingerprint"] = "chrome"
		}
		if alpn := q.Get("alpn"); alpn != "" {
			p["alpn"] = strings.Split(alpn, ",")
		}
		if pin := q.Get("pcs"); pin != "" {
			normalized, e := normalizeCertificatePin(pin)
			if e != nil {
				return nil, e
			}
			p["fingerprint"] = normalized
		} else if pin := q.Get("pinSHA256"); pin != "" {
			normalized, e := normalizeCertificatePin(pin)
			if e != nil {
				return nil, e
			}
			p["fingerprint"] = normalized
		}
	}
	if security == "reality" {
		key, e := base64.RawURLEncoding.DecodeString(q.Get("pbk"))
		if e != nil || len(key) != 32 {
			return nil, fmt.Errorf("invalid_reality_key")
		}
		sid := q.Get("sid")
		if _, e := hex.DecodeString(sid); e != nil || len(sid) > 16 {
			return nil, fmt.Errorf("invalid_reality_short_id")
		}
		p["reality-opts"] = map[string]any{"public-key": q.Get("pbk"), "short-id": sid}
	}
	if flow := q.Get("flow"); flow != "" {
		if protocol != "vless" || flow != "xtls-rprx-vision" {
			return nil, fmt.Errorf("unsupported_clash_flow")
		}
		// Mihomo 的 flow 只适用于 TCP/RAW；VLESS Encryption 下的跨传输
		// Vision 在 Clash 中省略，仍保留 encryption 字段。
		if transport == "tcp" || transport == "raw" {
			p["flow"] = flow
		}
	}
	if protocol == "vless" {
		if enc := strings.TrimSpace(q.Get("encryption")); enc != "" && enc != "none" {
			p["encryption"] = enc
		}
	}
	// XUDP gives Mihomo a UDP path that also works over XHTTP; it is a
	// separate wire encoding from the proxy protocol and is safe for TCP too.
	if protocol == "vless" || protocol == "vmess" {
		p["packet-encoding"] = "xudp"
	}
	return p, nil
}

// clashHysteria2 把 hysteria2:// 链接转换成 Mihomo 的 hysteria2 代理。
func clashHysteria2(u *url.URL) (map[string]any, error) {
	port := 443
	if p := u.Port(); p != "" {
		n, e := strconv.Atoi(p)
		if e != nil || n < 1 || n > 65535 {
			return nil, fmt.Errorf("invalid_share_port")
		}
		port = n
	}
	auth := u.User.Username()
	if pass, ok := u.User.Password(); ok {
		auth += ":" + pass
	}
	if auth == "" {
		return nil, fmt.Errorf("missing_share_credential")
	}
	q := u.Query()
	proxy := map[string]any{"name": u.Fragment, "type": "hysteria2", "server": u.Hostname(), "port": port, "password": auth, "udp": true}
	if sni := strings.TrimSpace(q.Get("sni")); sni != "" {
		proxy["sni"] = sni
	}
	if pin := strings.TrimSpace(q.Get("pinSHA256")); pin != "" {
		normalized, e := normalizeCertificatePin(pin)
		if e != nil {
			return nil, e
		}
		proxy["fingerprint"] = normalized
	} else if strings.EqualFold(q.Get("insecure"), "1") || strings.EqualFold(q.Get("insecure"), "true") {
		proxy["skip-cert-verify"] = true
	}
	if obfs := strings.TrimSpace(q.Get("obfs")); obfs != "" {
		proxy["obfs"] = obfs
		if pass := q.Get("obfs-password"); pass != "" {
			proxy["obfs-password"] = pass
		}
	}
	if ports := strings.TrimSpace(q.Get("ports")); ports != "" {
		proxy["ports"] = ports
	}
	if hop := strings.TrimSpace(q.Get("hop-interval")); hop != "" {
		n, e := strconv.Atoi(hop)
		if e != nil || n < 5 {
			return nil, fmt.Errorf("invalid_hop_interval")
		}
		proxy["hop-interval"] = n
	}
	if up := strings.TrimSpace(q.Get("up")); up != "" {
		proxy["up"] = up
	}
	if down := strings.TrimSpace(q.Get("down")); down != "" {
		proxy["down"] = down
	}
	if alpn := strings.TrimSpace(q.Get("alpn")); alpn != "" {
		proxy["alpn"] = strings.Split(alpn, ",")
	}
	return proxy, nil
}

func clashWSOpts(q url.Values, upgrade bool) map[string]any {
	path := q.Get("path")
	if path == "" {
		path = "/"
	}
	opts := map[string]any{"path": path}
	if host := q.Get("host"); host != "" {
		opts["headers"] = map[string]string{"Host": host}
	}
	if upgrade {
		opts["v2ray-http-upgrade"] = true
	}
	return opts
}

func clashXHTTPOpts(q url.Values) map[string]any {
	path := q.Get("path")
	if path == "" {
		path = "/"
	}
	opts := map[string]any{"path": path}
	if host := q.Get("host"); host != "" {
		opts["host"] = host
	}
	if mode := q.Get("mode"); mode != "" {
		opts["mode"] = mode
	}
	return opts
}

func normalizeCertificatePin(raw string) (string, error) {
	b, err := hex.DecodeString(strings.ReplaceAll(strings.TrimSpace(raw), ":", ""))
	if err != nil || len(b) != 32 {
		return "", fmt.Errorf("invalid_certificate_pin")
	}
	return hex.EncodeToString(b), nil
}

// JSON is a YAML 1.2 document and avoids injecting user names or credentials
// into YAML syntax. Mihomo consumes this complete profile directly.
func renderClash(links []string) ([]byte, []string, error) {
	proxies := []map[string]any{}
	names := []string{}
	omitted := []string{}
	used := map[string]bool{"vgatebox": true, "DIRECT": true, "REJECT": true, "GLOBAL": true}
	for i, link := range links {
		p, err := clashProxy(link)
		if err != nil {
			var unsupported *clashUnsupportedError
			if errors.As(err, &unsupported) {
				omitted = append(omitted, fmt.Sprintf("%d", i+1))
				continue
			}
			return nil, nil, fmt.Errorf("第 %d 个入口无法导出 Clash 配置（%s）", i+1, err)
		}
		name, _ := p["name"].(string)
		name = strings.TrimSpace(name)
		if name == "" {
			name = fmt.Sprintf("节点 %02d", i+1)
		}
		base := name
		for n := 2; used[name]; n++ {
			name = fmt.Sprintf("%s · %d", base, n)
		}
		used[name] = true
		p["name"] = name
		names = append(names, name)
		proxies = append(proxies, p)
	}
	if len(proxies) == 0 {
		return nil, omitted, fmt.Errorf("没有 Mihomo 可表达的节点")
	}
	blob, err := json.MarshalIndent(map[string]any{"mode": "rule", "log-level": "warning", "ipv6": false, "proxies": proxies, "proxy-groups": []any{map[string]any{"name": "vgatebox", "type": "select", "proxies": names}}, "rules": []string{"MATCH,vgatebox"}}, "", "  ")
	return blob, omitted, err
}
