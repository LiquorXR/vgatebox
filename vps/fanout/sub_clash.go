package main

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

type clashUnsupportedError struct{ reason string }

func (e *clashUnsupportedError) Error() string { return e.reason }

// clashProxy 把一个 VLESS 分享链接转成 mihomo 出站配置。
// 面板与第三方接入只生成 vless://，其他协议在导出前就被拒绝。
func clashProxy(link string) (map[string]any, error) {
	u, err := url.Parse(link)
	if err != nil || u.User == nil || u.Hostname() == "" {
		return nil, fmt.Errorf("invalid_share_link")
	}
	if !strings.EqualFold(u.Scheme, "vless") {
		return nil, fmt.Errorf("unsupported_clash_protocol")
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port < 1 || port > 65535 {
		return nil, fmt.Errorf("invalid_share_port")
	}
	uuid := u.User.Username()
	if uuid == "" {
		return nil, fmt.Errorf("missing_share_credential")
	}
	q := u.Query()

	p := map[string]any{
		"name":   u.Fragment,
		"type":   "vless",
		"server": u.Hostname(),
		"port":   port,
		"uuid":   uuid,
		"udp":    true,
	}

	transport := strings.ToLower(strings.TrimSpace(q.Get("type")))
	if transport == "" {
		transport = "tcp"
	}
	switch transport {
	case "tcp", "raw":
		p["network"] = "tcp"
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
		p["network"] = "xhttp"
		p["xhttp-opts"] = clashXHTTPOpts(q)
	case "mkcp", "kcp":
		return nil, &clashUnsupportedError{reason: "Mihomo cannot represent the mKCP transport"}
	default:
		return nil, fmt.Errorf("unsupported_clash_transport")
	}

	security := strings.ToLower(strings.TrimSpace(q.Get("security")))
	if security == "" {
		security = "none"
	}
	if security != "none" && security != "tls" && security != "reality" {
		return nil, fmt.Errorf("unsupported_clash_security")
	}
	if security != "none" {
		p["tls"], p["skip-cert-verify"] = true, false
		if sni := q.Get("sni"); sni != "" {
			p["servername"] = sni
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
		// Xray 26.9+ 的 REALITY 服务端要求 ClientHello 携带 X25519MLKEM768；
		// Mihomo 默认仍会移除该 key share，显式开启后才能完成认证。
		p["reality-opts"] = map[string]any{
			"public-key":             q.Get("pbk"),
			"short-id":               sid,
			"support-x25519mlkem768": true,
		}
	}
	// XHTTP 在 TLS/REALITY 下默认走 h2；显式写出与官方示例一致。
	if transport == "xhttp" && security != "none" {
		if _, ok := p["alpn"]; !ok {
			p["alpn"] = []string{"h2"}
		}
	}
	if flow := strings.TrimSpace(q.Get("flow")); flow != "" {
		if flow != "xtls-rprx-vision" {
			return nil, fmt.Errorf("unsupported_clash_flow")
		}
		// flow 是 VLESS 请求头里的 addons，与传输层无关。Xray 服务端在账号
		// 配置了 Vision 时会拒绝没有 flow 的客户端，所以所有传输都要写出。
		p["flow"] = flow
	}
	if enc := strings.TrimSpace(q.Get("encryption")); enc != "" && enc != "none" {
		p["encryption"] = enc
	}
	// XUDP gives Mihomo a UDP path that also works over XHTTP; it is a
	// separate wire encoding from the proxy protocol and is safe for TCP too.
	p["packet-encoding"] = "xudp"
	return p, nil
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

// yamlField 是保持官方示例字段顺序的映射项；yamlMap 与 yamlSeq 让输出不必
// 依赖 map 的随机遍历顺序，也不需要引入第三方 YAML 库。
type yamlField struct {
	key   string
	value any
}

type yamlMap []yamlField

type yamlSeq []yamlMap

// clashProxyOrder 与 mihomo 官方 VLESS/TLS/transport 文档示例的字段顺序一致：
// proxies 示例先出 tls/servername/alpn/fingerprint/client-fingerprint/
// skip-cert-verify/reality-opts，再出 encryption/network，最后出各传输 opts。
var clashProxyOrder = []string{
	"name", "type", "server", "port", "udp", "uuid",
	"flow", "packet-encoding",
	"tls", "servername", "alpn", "fingerprint", "client-fingerprint",
	"skip-cert-verify", "reality-opts", "encryption", "network",
	"ws-opts", "grpc-opts", "xhttp-opts",
}

// clashOptOrder 是嵌套配置块的字段顺序；strings.Split 之外的新字段会按字母
// 顺序排在后面，保证输出仍然稳定。
var clashOptOrder = map[string][]string{
	"reality-opts": {"public-key", "short-id", "support-x25519mlkem768"},
	"ws-opts":      {"path", "headers", "v2ray-http-upgrade"},
	"grpc-opts":    {"grpc-service-name"},
	"xhttp-opts":   {"path", "host", "mode"},
	"headers":      {"Host"},
}

func clashYAMLMap(prefix string, m map[string]any) yamlMap {
	order := clashOptOrder[prefix]
	if prefix == "" {
		order = clashProxyOrder
	}
	out := make(yamlMap, 0, len(m))
	used := make(map[string]bool, len(m))
	add := func(key string) {
		value, ok := m[key]
		if !ok {
			return
		}
		used[key] = true
		switch nested := value.(type) {
		case map[string]any:
			out = append(out, yamlField{key: key, value: clashYAMLMap(key, nested)})
		case map[string]string:
			converted := make(map[string]any, len(nested))
			for k, v := range nested {
				converted[k] = v
			}
			out = append(out, yamlField{key: key, value: clashYAMLMap(key, converted)})
		default:
			out = append(out, yamlField{key: key, value: value})
		}
	}
	for _, key := range order {
		add(key)
	}
	rest := make([]string, 0, len(m))
	for key := range m {
		if !used[key] {
			rest = append(rest, key)
		}
	}
	sort.Strings(rest)
	for _, key := range rest {
		add(key)
	}
	return out
}

// yamlQuote 输出 YAML 双引号标量。JSON 的字符串转义是 YAML 双引号语法的子集，
// 因此节点名、密码里的冒号、井号、引号或换行不会破坏文档结构。
func yamlQuote(s string) string {
	blob, err := json.Marshal(s)
	if err != nil {
		return `""`
	}
	return string(blob)
}

// isPlainScalar 判断字符串能否按 YAML 平面标量原样输出。
//
// 官方示例对枚举/主机名/路径使用平面写法（type: vless、mode: rule），
// 对含空格/中文/逗号/冒号/井号的值使用双引号。平面写法语义不变但更贴近示例。
// 布尔/null/数字字面量必须加引号，否则会被解析成非字符串类型。
func isPlainScalar(s string) bool {
	if s == "" {
		return false
	}
	switch strings.ToLower(s) {
	case "true", "false", "yes", "no", "on", "off", "null", "~", "nil":
		return false
	}
	if _, err := strconv.ParseFloat(s, 64); err == nil {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9',
			r == '_' || r == '-' || r == '.' || r == '/':
		default:
			return false
		}
	}
	c := s[0]
	if c == '/' {
		return true
	}
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')
}

// yamlScalar 优先按官方示例的平面风格输出，必要时才加双引号。
func yamlScalar(s string) string {
	if isPlainScalar(s) {
		return s
	}
	return yamlQuote(s)
}

func writeYAMLMap(b *strings.Builder, m yamlMap, indent int) {
	pad := strings.Repeat(" ", indent)
	for _, f := range m {
		b.WriteString(pad)
		writeYAMLField(b, f, indent)
	}
}

func writeYAMLField(b *strings.Builder, f yamlField, indent int) {
	b.WriteString(f.key)
	b.WriteString(":")
	switch value := f.value.(type) {
	case string:
		b.WriteString(" ")
		b.WriteString(yamlScalar(value))
		b.WriteString("\n")
	case bool:
		b.WriteString(" ")
		b.WriteString(strconv.FormatBool(value))
		b.WriteString("\n")
	case int:
		b.WriteString(" ")
		b.WriteString(strconv.Itoa(value))
		b.WriteString("\n")
	case []string:
		b.WriteString("\n")
		for _, item := range value {
			b.WriteString(strings.Repeat(" ", indent+2))
			b.WriteString("- ")
			b.WriteString(yamlScalar(item))
			b.WriteString("\n")
		}
	case yamlMap:
		b.WriteString("\n")
		writeYAMLMap(b, value, indent+2)
	case yamlSeq:
		b.WriteString("\n")
		for _, item := range value {
			itemIndent := indent + 2
			if len(item) == 0 {
				b.WriteString(strings.Repeat(" ", itemIndent))
				b.WriteString("- {}\n")
				continue
			}
			b.WriteString(strings.Repeat(" ", itemIndent))
			b.WriteString("- ")
			writeYAMLField(b, item[0], itemIndent+2)
			writeYAMLMap(b, item[1:], itemIndent+2)
		}
	default:
		b.WriteString(" ")
		b.WriteString(yamlScalar(fmt.Sprint(value)))
		b.WriteString("\n")
	}
}

// renderClash 输出 mihomo 官方示例形态的块状 YAML。所有字符串都经过转义，
// 节点名或凭据不会注入 YAML 语法。
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

	proxyItems := make(yamlSeq, 0, len(proxies))
	for _, p := range proxies {
		proxyItems = append(proxyItems, clashYAMLMap("", p))
	}
	doc := yamlMap{
		{key: "mixed-port", value: 7890},
		{key: "allow-lan", value: false},
		{key: "mode", value: "rule"},
		{key: "log-level", value: "warning"},
		{key: "ipv6", value: false},
		{key: "profile", value: yamlMap{
			{key: "store-selected", value: true},
		}},
		{key: "proxies", value: proxyItems},
		{key: "proxy-groups", value: yamlSeq{{
			{key: "name", value: "vgatebox"},
			{key: "type", value: "select"},
			{key: "proxies", value: names},
		}}},
		{key: "rules", value: []string{"MATCH,vgatebox"}},
	}
	var b strings.Builder
	writeYAMLMap(&b, doc, 0)
	return []byte(b.String()), omitted, nil
}
