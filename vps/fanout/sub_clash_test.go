package main

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

type clashMatrixEntry struct {
	network  string
	security string
}

// clashMatrix 是面板允许创建的全部「传输 × 安全层」组合。
func clashMatrix() []clashMatrixEntry {
	securities := map[string][]string{
		"tcp":         {"none", "tls", "reality"},
		"ws":          {"none", "tls"},
		"grpc":        {"none", "tls", "reality"},
		"httpupgrade": {"none", "tls"},
		"xhttp":       {"none", "tls", "reality"},
	}
	var out []clashMatrixEntry
	for _, network := range []string{"tcp", "ws", "grpc", "httpupgrade", "xhttp"} {
		for _, security := range securities[network] {
			out = append(out, clashMatrixEntry{network: network, security: security})
		}
	}
	return out
}

func clashRealityKey(t *testing.T) string {
	t.Helper()
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes())
}

func clashLink(network, security, name, key string, extra url.Values) string {
	q := url.Values{}
	q.Set("type", network)
	q.Set("security", security)
	switch network {
	case "ws", "httpupgrade", "xhttp":
		q.Set("path", "/"+network)
		q.Set("host", "cdn.example")
	case "grpc":
		q.Set("serviceName", "svc")
	}
	if security != "none" {
		q.Set("sni", "example.com")
		q.Set("fp", "chrome")
	}
	if security == "reality" {
		q.Set("pbk", key)
		q.Set("sid", "12345678")
	}
	for key, values := range extra {
		for _, value := range values {
			q.Add(key, value)
		}
	}
	return "vless://fbaea75a-e91c-4ad9-9410-7e43aa967766@203.0.113.1:443?" + q.Encode() + "#" + url.PathEscape(name)
}

var (
	clashProxyNamePattern = regexp.MustCompile(`(?m)^  - name: (".*")$`)
	clashGroupNamePattern = regexp.MustCompile(`(?m)^      - (".*")$`)
)

func clashProxiesSection(body string) string {
	start := strings.Index(body, "\nproxies:\n")
	if start < 0 {
		return body
	}
	tail := body[start+1:]
	if end := strings.Index(tail, "\nproxy-groups:\n"); end >= 0 {
		return tail[:end]
	}
	return tail
}

func clashGroupSection(body string) string {
	start := strings.Index(body, "\nproxy-groups:\n")
	if start < 0 {
		return ""
	}
	tail := body[start+1:]
	if end := strings.Index(tail, "\nrules:\n"); end >= 0 {
		return tail[:end]
	}
	return tail
}

func renderedNames(t *testing.T, pattern *regexp.Regexp, body string) []string {
	t.Helper()
	var out []string
	for _, match := range pattern.FindAllStringSubmatch(body, -1) {
		var name string
		if err := json.Unmarshal([]byte(match[1]), &name); err != nil {
			t.Fatalf("invalid quoted scalar %s: %v", match[1], err)
		}
		out = append(out, name)
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestClashProfileMatchesDocumentedShape 固定默认节点的完整输出，确保顶层字段、
// 字段顺序和官方 mihomo 配置示例一致。
func TestClashProfileMatchesDocumentedShape(t *testing.T) {
	key := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	link := "vless://fbaea75a-e91c-4ad9-9410-7e43aa967766@203.0.113.1:443?type=tcp&security=reality&flow=xtls-rprx-vision&sni=example.com&fp=chrome&pbk=" + key + "&sid=12345678#VPS%20%E6%9C%AC%E6%9C%BA%E7%BD%91%E7%BB%9C"
	blob, omitted, err := renderClash([]string{link})
	if err != nil || len(omitted) != 0 {
		t.Fatal(err, omitted)
	}
	want := `mixed-port: 7890
allow-lan: false
mode: rule
log-level: warning
ipv6: false
profile:
  store-selected: true
proxies:
  - name: "VPS 本机网络"
    type: vless
    server: 203.0.113.1
    port: 443
    udp: true
    uuid: fbaea75a-e91c-4ad9-9410-7e43aa967766
    flow: xtls-rprx-vision
    packet-encoding: xudp
    tls: true
    servername: example.com
    client-fingerprint: chrome
    skip-cert-verify: false
    reality-opts:
      public-key: ` + key + `
      short-id: "12345678"
      support-x25519mlkem768: true
    network: tcp
proxy-groups:
  - name: vgatebox
    type: select
    proxies:
      - "VPS 本机网络"
rules:
  - "MATCH,vgatebox"
`
	if string(blob) != want {
		t.Fatalf("unexpected profile:\n%s", blob)
	}
}

func TestClashProfilePreservesOrderAndUniqueNames(t *testing.T) {
	key := clashRealityKey(t)
	links := []string{
		clashLink("tcp", "reality", "VPS 本机网络", key, nil),
		clashLink("tcp", "reality", "自定义 · 示例", key, nil),
		clashLink("tcp", "reality", "🇯🇵 日本 01", key, nil),
		clashLink("tcp", "reality", "DIRECT", key, nil),
		clashLink("tcp", "reality", "VPS 本机网络", key, nil),
	}
	body, omitted, err := renderClash(links)
	if err != nil || len(omitted) != 0 {
		t.Fatal(err, omitted)
	}
	names := renderedNames(t, clashProxyNamePattern, clashProxiesSection(string(body)))
	want := []string{"VPS 本机网络", "自定义 · 示例", "🇯🇵 日本 01", "DIRECT · 2", "VPS 本机网络 · 2"}
	if !equalStrings(names, want) {
		t.Fatalf("proxy names changed: %q", names)
	}
	group := renderedNames(t, clashGroupNamePattern, clashGroupSection(string(body)))
	if !equalStrings(group, want) {
		t.Fatalf("group order changed: %q", group)
	}
	if !strings.Contains(string(body), "rules:\n  - \"MATCH,vgatebox\"\n") {
		t.Fatal("implicit fallback added")
	}
	if strings.Contains(string(body), "DIRECT\"") {
		t.Fatal("reserved name leaked as a selectable proxy")
	}
}

func TestClashQuotesNamesAndCredentials(t *testing.T) {
	name := `Name: #[test] "quoted" \ end`
	link := clashLink("tcp", "tls", name, "", nil)
	body, _, err := renderClash([]string{link})
	if err != nil {
		t.Fatal(err)
	}
	names := renderedNames(t, clashProxyNamePattern, clashProxiesSection(string(body)))
	if len(names) != 1 || names[0] != name {
		t.Fatalf("name was not escaped safely: %q", names)
	}
	if strings.Contains(string(body), `name: Name`) {
		t.Fatal("unquoted scalar produced ambiguous YAML")
	}
}

func TestClashTransportsPinAndCredentialsAreNotDowngraded(t *testing.T) {
	pin := strings.Repeat("ab", 32)
	link := "vless://fbaea75a-e91c-4ad9-9410-7e43aa967766@host.example:443?type=ws&security=tls&path=%2Fws&host=cdn.example&pinSHA256=" + pin + "#pin"
	p, err := clashProxy(link)
	if err != nil || p["fingerprint"] != pin || p["skip-cert-verify"] != false {
		t.Fatal("certificate pin lost", err)
	}
	ws := p["ws-opts"].(map[string]any)
	if ws["path"] != "/ws" || ws["headers"].(map[string]string)["Host"] != "cdn.example" {
		t.Fatal("websocket mapping lost", p)
	}
	for _, link := range []string{
		"vless://fbaea75a-e91c-4ad9-9410-7e43aa967766@host.example:443?type=tcp&security=reality&pbk=invalid&sid=12345678&sni=example.com",
		"vless://fbaea75a-e91c-4ad9-9410-7e43aa967766@host.example:443?type=tcp&security=reality&pbk=" + base64.RawURLEncoding.EncodeToString(make([]byte, 32)) + "&sid=xyz&sni=example.com",
		"vless://fbaea75a-e91c-4ad9-9410-7e43aa967766@host.example:443?type=tcp&security=tls&flow=other",
		"vless://fbaea75a-e91c-4ad9-9410-7e43aa967766@host.example:443?type=tcp&security=invalid",
		"vless://fbaea75a-e91c-4ad9-9410-7e43aa967766@host.example:443?type=hysteria&security=tls",
		"vmess://fbaea75a-e91c-4ad9-9410-7e43aa967766@host.example:443?type=tcp",
	} {
		if _, err := clashProxy(link); err == nil {
			t.Fatalf("unsupported or invalid configuration accepted: %s", link)
		}
	}
}

func TestClashXHTTPAndHTTPUpgradeMappings(t *testing.T) {
	key := clashRealityKey(t)
	link := clashLink("xhttp", "reality", "XHTTP", key, nil)
	p, err := clashProxy(link)
	if err != nil {
		t.Fatal(err)
	}
	xopts, ok := p["xhttp-opts"].(map[string]any)
	if !ok || p["network"] != "xhttp" || xopts["path"] != "/xhttp" || p["udp"] != true || p["packet-encoding"] != "xudp" {
		t.Fatal("xhttp mapping lost", p)
	}
	if opts, ok := p["alpn"].([]string); !ok || len(opts) != 1 || opts[0] != "h2" {
		t.Fatalf("xhttp default alpn missing: %v", p["alpn"])
	}
	if p["reality-opts"].(map[string]any)["short-id"] != "12345678" {
		t.Fatal("xhttp reality lost", p)
	}

	alpnLink := clashLink("xhttp", "tls", "XHTTP-ALPN", key, url.Values{"alpn": {"http/1.1"}})
	p, err = clashProxy(alpnLink)
	if err != nil || p["alpn"].([]string)[0] != "http/1.1" {
		t.Fatal("explicit alpn lost", err, p["alpn"])
	}

	upgradeLink := clashLink("httpupgrade", "tls", "HTTPUpgrade", key, url.Values{"pcs": {strings.Repeat("ab", 32)}})
	p, err = clashProxy(upgradeLink)
	if err != nil {
		t.Fatal(err)
	}
	ws, ok := p["ws-opts"].(map[string]any)
	if !ok || p["network"] != "ws" || ws["path"] != "/httpupgrade" || ws["v2ray-http-upgrade"] != true || p["fingerprint"] != strings.Repeat("ab", 32) {
		t.Fatal("httpupgrade mapping lost", p)
	}
	if ws["headers"].(map[string]string)["Host"] != "cdn.example" {
		t.Fatal("httpupgrade host lost", p)
	}

	mkcp := clashLink("mkcp", "none", "KCP", key, nil)
	body, omitted, err := renderClash([]string{link, mkcp})
	// 纯量按官方示例输出：XHTTP 这类安全字符走平面写法（name: XHTTP），
	// 含空格/中文的才加引号；此处只断言名字出现与否，不锁定引号。
	if err != nil || len(omitted) != 1 || !strings.Contains(string(body), "XHTTP") || strings.Contains(string(body), "KCP") {
		t.Fatal("mKCP entry was not skipped alone", err, omitted, string(body))
	}
}

func TestClashVLESSEncryptionAndFlow(t *testing.T) {
	key := clashRealityKey(t)
	enc := "mlkem768x25519plus.native.0rtt." + key
	link := clashLink("tcp", "reality", "ENC", key, url.Values{"encryption": {enc}, "flow": {"xtls-rprx-vision"}})
	p, err := clashProxy(link)
	if err != nil || p["encryption"] != enc || p["flow"] != "xtls-rprx-vision" {
		t.Fatalf("vless encryption or vision lost: %v %v", p["encryption"], p["flow"])
	}

	none := clashLink("tcp", "tls", "NONE", key, url.Values{"encryption": {"none"}})
	p, err = clashProxy(none)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := p["encryption"]; ok {
		t.Fatal("encryption=none should be omitted")
	}
}

// flow 是 VLESS 请求头的一部分，Xray 在账号配置了 Vision 时会拒绝空 flow，
// 因此所有传输都必须写着它。
func TestClashFlowIsEmittedForEveryTransport(t *testing.T) {
	key := clashRealityKey(t)
	enc := "mlkem768x25519plus.native.0rtt." + key
	for _, network := range []string{"tcp", "ws", "grpc", "httpupgrade", "xhttp"} {
		link := clashLink(network, "tls", network, key, url.Values{"encryption": {enc}, "flow": {"xtls-rprx-vision"}})
		p, err := clashProxy(link)
		if err != nil {
			t.Fatalf("%s: %v", network, err)
		}
		if p["flow"] != "xtls-rprx-vision" {
			t.Fatalf("%s: vision flow dropped", network)
		}
	}
}

func TestClashTransportMatrixMappings(t *testing.T) {
	key := clashRealityKey(t)
	for _, tc := range clashMatrix() {
		link := clashLink(tc.network, tc.security, tc.network+"-"+tc.security, key, nil)
		p, err := clashProxy(link)
		if err != nil {
			t.Fatalf("%s/%s: %v", tc.network, tc.security, err)
		}
		wantNetwork := tc.network
		if wantNetwork == "httpupgrade" {
			wantNetwork = "ws"
		}
		if p["network"] != wantNetwork {
			t.Fatalf("%s/%s: network = %v, want %s", tc.network, tc.security, p["network"], wantNetwork)
		}
		if tc.security == "none" {
			if _, ok := p["tls"]; ok {
				t.Fatalf("%s/%s: unexpected tls", tc.network, tc.security)
			}
			continue
		}
		if p["tls"] != true || p["skip-cert-verify"] != false {
			t.Fatalf("%s/%s: tls or verification downgraded", tc.network, tc.security)
		}
		if tc.security == "reality" {
			if _, ok := p["reality-opts"]; !ok {
				t.Fatalf("%s/%s: reality options lost", tc.network, tc.security)
			}
		}
	}
}

func TestClashSubscriptionEndpointAndAccess(t *testing.T) {
	useSettings(t)
	usePanel(t, &fakePanel{inbounds: []Inbound{{ID: 1, Enable: true}}})
	m := NewManager(20, t.TempDir())
	tok, _ := subToken()
	w := httptest.NewRecorder()
	handleSub(m)(w, httptest.NewRequest(http.MethodGet, "/sub?token="+tok+"&target=clash&bound=0", nil))
	if w.Code != 200 || !strings.Contains(w.Header().Get("Content-Type"), "yaml") || !strings.HasPrefix(w.Body.String(), "mixed-port: ") {
		t.Fatalf("clash endpoint unavailable: %d %q", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	handleSub(m)(w, httptest.NewRequest(http.MethodGet, "/sub?token=wrong&target=clash", nil))
	if w.Code != 404 {
		t.Fatal("format bypassed authentication")
	}
	w = httptest.NewRecorder()
	handleSub(m)(w, httptest.NewRequest(http.MethodGet, "/sub?token="+tok+"&target=clash&vpn=0&direct=0&third=0", nil))
	if w.Code != 404 {
		t.Fatal("empty selection exported a fallback configuration")
	}
}

func TestClashMihomoAcceptsGeneratedProfiles(t *testing.T) {
	binary, err := filepath.Abs(filepath.Join("..", "..", "tools", "bin", "mihomo"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(binary); err != nil {
		t.Skip("install the pinned official Mihomo test client")
	}
	key := clashRealityKey(t)
	enc := "mlkem768x25519plus.native.0rtt." + key
	cases := [][]string{}
	for _, tc := range clashMatrix() {
		cases = append(cases, []string{clashLink(tc.network, tc.security, tc.network+"-"+tc.security, key, nil)})
	}
	for _, network := range []string{"tcp", "ws", "grpc", "httpupgrade", "xhttp"} {
		cases = append(cases, []string{clashLink(network, "tls", "enc-"+network, key, url.Values{
			"encryption": {enc},
			"flow":       {"xtls-rprx-vision"},
		})})
	}
	cases = append(cases, []string{
		clashLink("tcp", "reality", "one", key, nil),
		clashLink("tcp", "reality", "two", key, nil),
		clashLink("tcp", "reality", "three", key, nil),
	})
	for _, links := range cases {
		dir := t.TempDir()
		blob, omitted, err := renderClash(links)
		if err != nil || len(omitted) != 0 {
			t.Fatal(err, omitted)
		}
		path := filepath.Join(dir, "profile.yaml")
		if err := os.WriteFile(path, blob, 0600); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		out, err := exec.CommandContext(ctx, binary, "-t", "-d", dir, "-f", path).CombinedOutput()
		cancel()
		if err != nil {
			if len(out) > 1200 {
				out = out[:1200]
			}
			t.Fatalf("pinned Mihomo rejected public fixture %v: %v %s", links, err, out)
		}
	}
}
