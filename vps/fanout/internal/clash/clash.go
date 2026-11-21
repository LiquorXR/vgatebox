// Package clash 是 VLESS 分享链接到 Mihomo/Clash 出站配置的纯转换层。
//
// 从主包 sub_clash.go 下沉而来：仅依赖标准库，无网络、无磁盘、无 root，
// 可在 Windows/macOS 本机直接 `go test ./internal/clash` 与 Fuzz。
// 主包保留薄包装以兼容现有调用方与 62 路由契约测试。
package clash

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

// UnsupportedError 表示 Mihomo 无法表达的传输（如 mKCP），调用方可跳过该节点。
type UnsupportedError struct{ Reason string }

func (e *UnsupportedError) Error() string { return e.Reason }

// Proxy 把一个 VLESS 分享链接转成 mihomo 出站配置。
func Proxy(link string) (map[string]any, error) {
	u, port, uuid, err := ParseLink(link)
	if err != nil {
		return nil, err
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
	transport, err := ApplyTransport(p, q)
	if err != nil {
		return nil, err
	}
	if err := ApplySecurity(p, q, transport); err != nil {
		return nil, err
	}
	if err := ApplyVLESSOptions(p, q); err != nil {
		return nil, err
	}
	p["packet-encoding"] = "xudp"
	return p, nil
}

// ParseLink 解析分享链接并校验协议与端口。
func ParseLink(link string) (*url.URL, int, string, error) {
	u, err := url.Parse(link)
	if err != nil || u.User == nil || u.Hostname() == "" {
		return nil, 0, "", fmt.Errorf("invalid_share_link")
	}
	if !strings.EqualFold(u.Scheme, "vless") {
		return nil, 0, "", fmt.Errorf("unsupported_clash_protocol")
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port < 1 || port > 65535 {
		return nil, 0, "", fmt.Errorf("invalid_share_port")
	}
	uuid := u.User.Username()
	if uuid == "" {
		return nil, 0, "", fmt.Errorf("missing_share_credential")
	}
	return u, port, uuid, nil
}

// ApplyTransport 映射传输层，mKCP 返回 *UnsupportedError 以便调用方跳过。
func ApplyTransport(p map[string]any, q url.Values) (string, error) {
	transport := strings.ToLower(strings.TrimSpace(q.Get("type")))
	if transport == "" {
		transport = "tcp"
	}
	switch transport {
	case "tcp", "raw":
		p["network"] = "tcp"
	case "ws":
		p["network"] = "ws"
		p["ws-opts"] = WSOpts(q, false)
	case "httpupgrade", "http-upgrade":
		p["network"] = "ws"
		p["ws-opts"] = WSOpts(q, true)
	case "grpc":
		p["network"] = "grpc"
		p["grpc-opts"] = map[string]any{"grpc-service-name": strings.TrimPrefix(q.Get("serviceName"), "/")}
	case "xhttp":
		p["network"] = "xhttp"
		p["xhttp-opts"] = XHTTPOpts(q)
	case "mkcp", "kcp":
		return "", &UnsupportedError{Reason: "Mihomo cannot represent the mKCP transport"}
	default:
		return "", fmt.Errorf("unsupported_clash_transport")
	}
	return transport, nil
}

// ApplySecurity 映射 TLS/REALITY 安全层。
func ApplySecurity(p map[string]any, q url.Values, transport string) error {
	security := strings.ToLower(strings.TrimSpace(q.Get("security")))
	if security == "" {
		security = "none"
	}
	switch security {
	case "none", "tls", "reality":
	default:
		return fmt.Errorf("unsupported_clash_security")
	}
	if security != "none" {
		if err := ApplyTLS(p, q, security); err != nil {
			return err
		}
	}
	if security == "reality" {
		if err := ApplyReality(p, q); err != nil {
			return err
		}
	}
	if transport == "xhttp" && security != "none" {
		if _, ok := p["alpn"]; !ok {
			p["alpn"] = []string{"h2"}
		}
	}
	return nil
}

// ApplyTLS 写入 TLS 通用字段。
func ApplyTLS(p map[string]any, q url.Values, security string) error {
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
	pin, ok, err := CertificatePin(q)
	if err != nil {
		return err
	}
	if ok {
		p["fingerprint"] = pin
	}
	return nil
}

// CertificatePin 解析自签证书指纹。
func CertificatePin(q url.Values) (string, bool, error) {
	raw := q.Get("pcs")
	if raw == "" {
		raw = q.Get("pinSHA256")
	}
	if raw == "" {
		return "", false, nil
	}
	normalized, err := NormalizeCertificatePin(raw)
	if err != nil {
		return "", false, err
	}
	return normalized, true, nil
}

// ApplyReality 写入 REALITY 字段。
func ApplyReality(p map[string]any, q url.Values) error {
	key, err := base64.RawURLEncoding.DecodeString(q.Get("pbk"))
	if err != nil || len(key) != 32 {
		return fmt.Errorf("invalid_reality_key")
	}
	sid := q.Get("sid")
	if _, err := hex.DecodeString(sid); err != nil || len(sid) > 16 {
		return fmt.Errorf("invalid_reality_short_id")
	}
	p["reality-opts"] = map[string]any{
		"public-key":             q.Get("pbk"),
		"short-id":               sid,
		"support-x25519mlkem768": true,
	}
	return nil
}

// ApplyVLESSOptions 写入 flow/encryption。
func ApplyVLESSOptions(p map[string]any, q url.Values) error {
	if flow := strings.TrimSpace(q.Get("flow")); flow != "" {
		if flow != "xtls-rprx-vision" {
			return fmt.Errorf("unsupported_clash_flow")
		}
		p["flow"] = flow
	}
	if enc := strings.TrimSpace(q.Get("encryption")); enc != "" && enc != "none" {
		p["encryption"] = enc
	}
	return nil
}

// WSOpts 构造 WS 参数。
func WSOpts(q url.Values, upgrade bool) map[string]any {
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

// XHTTPOpts 构造 XHTTP 参数。
func XHTTPOpts(q url.Values) map[string]any {
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

// NormalizeCertificatePin 归一化证书指纹为小写 hex。
func NormalizeCertificatePin(raw string) (string, error) {
	b, err := hex.DecodeString(strings.ReplaceAll(strings.TrimSpace(raw), ":", ""))
	if err != nil || len(b) != 32 {
		return "", fmt.Errorf("invalid_certificate_pin")
	}
	return hex.EncodeToString(b), nil
}

// IsUnsupported 判断是否为 Mihomo 不可表达的传输。
func IsUnsupported(err error) bool {
	var unsupported *UnsupportedError
	return errors.As(err, &unsupported)
}

type yamlField struct {
	key   string
	value any
}

type yamlMap []yamlField

type yamlSeq []yamlMap

var proxyOrder = []string{
	"name", "type", "server", "port", "udp", "uuid",
	"flow", "packet-encoding",
	"tls", "servername", "alpn", "fingerprint", "client-fingerprint",
	"skip-cert-verify", "reality-opts", "encryption", "network",
	"ws-opts", "grpc-opts", "xhttp-opts",
}

var optOrder = map[string][]string{
	"reality-opts": {"public-key", "short-id", "support-x25519mlkem768"},
	"ws-opts":      {"path", "headers", "v2ray-http-upgrade"},
	"grpc-opts":    {"grpc-service-name"},
	"xhttp-opts":   {"path", "host", "mode"},
	"headers":      {"Host"},
}

func buildYAMLMap(prefix string, m map[string]any) yamlMap {
	order := optOrder[prefix]
	if prefix == "" {
		order = proxyOrder
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
			out = append(out, yamlField{key: key, value: buildYAMLMap(key, nested)})
		case map[string]string:
			converted := make(map[string]any, len(nested))
			for k, v := range nested {
				converted[k] = v
			}
			out = append(out, yamlField{key: key, value: buildYAMLMap(key, converted)})
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

func yamlQuote(s string) string {
	blob, err := json.Marshal(s)
	if err != nil {
		return `""`
	}
	return string(blob)
}

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

// Render 输出 mihomo 官方示例形态的块状 YAML。
func Render(links []string) ([]byte, []string, error) {
	proxies := []map[string]any{}
	names := []string{}
	omitted := []string{}
	used := map[string]bool{"vgatebox": true, "DIRECT": true, "REJECT": true, "GLOBAL": true}
	for i, link := range links {
		p, err := Proxy(link)
		if err != nil {
			if IsUnsupported(err) {
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
		proxyItems = append(proxyItems, buildYAMLMap("", p))
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
