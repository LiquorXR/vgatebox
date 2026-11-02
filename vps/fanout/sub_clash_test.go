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
	"strings"
	"testing"
	"time"
)

func clashRealityFixture(name string) string {
	return "vless://fbaea75a-e91c-4ad9-9410-7e43aa967766@203.0.113.1:443?type=tcp&security=reality&flow=xtls-rprx-vision&sni=example.com&fp=chrome&pbk=" + base64.RawURLEncoding.EncodeToString(make([]byte, 32)) + "&sid=12345678#" + url.PathEscape(name)
}

func TestClashProfilePreservesRealityOrderNamesAndNoDirectFallback(t *testing.T) {
	links := []string{clashRealityFixture("VPS 本机直出"), clashRealityFixture("自定义 · 示例"), clashRealityFixture("🇯🇵 日本 01"), clashRealityFixture("DIRECT"), clashRealityFixture("VPS 本机直出")}
	blob, omitted, err := renderClash(links)
	if err != nil || len(omitted) != 0 {
		t.Fatal(err, omitted)
	}
	var cfg struct {
		Proxies []map[string]any `json:"proxies"`
		Groups  []struct {
			Proxies []string `json:"proxies"`
			Type    string   `json:"type"`
		} `json:"proxy-groups"`
		Rules []string `json:"rules"`
	}
	if json.Unmarshal(blob, &cfg) != nil || len(cfg.Proxies) != 5 || len(cfg.Groups) != 1 {
		t.Fatal("invalid profile")
	}
	if cfg.Groups[0].Type != "select" || cfg.Rules[0] != "MATCH,vgatebox" {
		t.Fatal("implicit fallback added")
	}
	seen := map[string]bool{}
	for i, p := range cfg.Proxies {
		name := p["name"].(string)
		if seen[name] || name == "DIRECT" {
			t.Fatal("reserved or duplicate name")
		}
		seen[name] = true
		if cfg.Groups[0].Proxies[i] != name || p["udp"] != true || p["skip-cert-verify"] != false || p["client-fingerprint"] != "chrome" || p["flow"] != "xtls-rprx-vision" || p["packet-encoding"] != "xudp" {
			t.Fatal("transport or ordering changed")
		}
		reality := p["reality-opts"].(map[string]any)
		if reality["short-id"] != "12345678" || reality["support-x25519mlkem768"] != true {
			t.Fatal("REALITY credentials or ML-KEM compatibility lost")
		}
	}
}

func TestClashTransportsPinAndCredentialsAreNotDowngraded(t *testing.T) {
	links := []string{
		"vless://fbaea75a-e91c-4ad9-9410-7e43aa967766@host.example:443?type=ws&security=tls&path=%2Fws&host=cdn.example&pinSHA256=" + strings.Repeat("ab", 32) + "#Name%3A%20%23%5Btest%5D",
		"trojan://" + url.User("p@ss:#/?").String() + "@host.example:443?security=tls&type=grpc&serviceName=service&sni=host.example#test",
	}
	p, err := clashProxy(links[0])
	if err != nil || p["fingerprint"] != strings.Repeat("ab", 32) || p["skip-cert-verify"] != false {
		t.Fatal("certificate pin lost", err)
	}
	p, err = clashProxy(links[1])
	if err != nil || p["password"] != "p@ss:#/?" || p["network"] != "grpc" {
		t.Fatal("credential escaping or transport failed", err)
	}
	for _, pair := range [][2]string{{"pbk=" + base64.RawURLEncoding.EncodeToString(make([]byte, 32)), "pbk=invalid"}, {"flow=xtls-rprx-vision", "flow=other"}, {"security=reality", "security=invalid"}} {
		link := strings.Replace(clashRealityFixture("test"), pair[0], pair[1], 1)
		if _, _, err := renderClash([]string{link}); err == nil {
			t.Fatal("unsupported or invalid configuration accepted")
		}
	}
}

func TestClashStandardVmessShareAndScopedEndpoint(t *testing.T) {
	jsonShare := `{"v":"2","ps":"VMess","add":"host.example","port":"443","id":"fbaea75a-e91c-4ad9-9410-7e43aa967766","aid":"0","net":"ws","path":"/proxy","tls":"tls","sni":"host.example"}`
	p, err := clashProxy("vmess://" + base64.StdEncoding.EncodeToString([]byte(jsonShare)))
	if err != nil || p["type"] != "vmess" || p["network"] != "ws" || p["tls"] != true {
		t.Fatal("standard VMess rejected", err)
	}
	useSettings(t)
	usePanel(t, &fakePanel{inbounds: []Inbound{{ID: 1, Enable: true}}})
	m := NewManager(20, t.TempDir())
	tok, _ := subToken()
	w := httptest.NewRecorder()
	handleSub(m)(w, httptest.NewRequest(http.MethodGet, "/sub?token="+tok+"&target=clash&bound=0", nil))
	if w.Code != 200 || !strings.Contains(w.Header().Get("Content-Type"), "yaml") || !strings.Contains(w.Body.String(), `"proxies"`) {
		t.Fatal("clash endpoint unavailable", w.Code)
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

func TestClashXHTTPAndHTTPUpgradeMappings(t *testing.T) {
	pbk := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	pin := strings.Repeat("ab", 32)
	vlessXHTTP := "vless://fbaea75a-e91c-4ad9-9410-7e43aa967766@host.example:443?type=xhttp&security=reality&sni=example.com&fp=chrome&pbk=" + pbk + "&sid=12345678&path=%2Fxhttp#XHTTP"
	p, err := clashProxy(vlessXHTTP)
	if err != nil {
		t.Fatal(err)
	}
	xopts, ok := p["xhttp-opts"].(map[string]any)
	if !ok || p["network"] != "xhttp" || xopts["path"] != "/xhttp" || p["udp"] != true || p["packet-encoding"] != "xudp" {
		t.Fatal("xhttp mapping lost", p)
	}
	if p["reality-opts"].(map[string]any)["short-id"] != "12345678" {
		t.Fatal("xhttp reality lost", p)
	}

	vlessHU := "vless://fbaea75a-e91c-4ad9-9410-7e43aa967766@host.example:443?type=httpupgrade&security=tls&path=%2Fup&host=cdn.example&sni=host.example&pcs=" + pin + "#HTTPUpgrade"
	p, err = clashProxy(vlessHU)
	if err != nil {
		t.Fatal(err)
	}
	ws, ok := p["ws-opts"].(map[string]any)
	if !ok || p["network"] != "ws" || ws["path"] != "/up" || ws["v2ray-http-upgrade"] != true || p["fingerprint"] != pin || p["udp"] != true {
		t.Fatal("httpupgrade mapping lost", p)
	}
	if ws["headers"].(map[string]string)["Host"] != "cdn.example" {
		t.Fatal("httpupgrade host lost", p)
	}
	for _, link := range []string{
		"vmess://fbaea75a-e91c-4ad9-9410-7e43aa967766@host.example:443?type=httpupgrade&security=tls&path=%2Fup&host=cdn.example#VMessUpgrade",
		"trojan://public-test-password@host.example:443?type=httpupgrade&security=tls&path=%2Fup&host=cdn.example#TrojanUpgrade",
		"vmess://fbaea75a-e91c-4ad9-9410-7e43aa967766@host.example:443?type=tcp&security=reality&pbk=" + pbk + "&sid=12345678&sni=example.com#VMessReality",
		"trojan://public-test-password@host.example:443?type=tcp&security=reality&pbk=" + pbk + "&sid=12345678&sni=example.com#TrojanReality",
	} {
		p, err := clashProxy(link)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(link, "type=httpupgrade") && p["ws-opts"].(map[string]any)["v2ray-http-upgrade"] != true {
			t.Fatal("httpupgrade flag lost", p)
		}
		if strings.Contains(link, "security=reality") {
			if _, ok := p["reality-opts"].(map[string]any); !ok {
				t.Fatal("reality mapping lost", p)
			}
		}
	}

	vmessXHTTP := "vmess://fbaea75a-e91c-4ad9-9410-7e43aa967766@host.example:443?type=xhttp&security=tls&sni=host.example#VMessXHTTP"
	if _, err := clashProxy(vmessXHTTP); err == nil {
		t.Fatal("VMess XHTTP should be marked unsupported")
	}
	trojanNone := "trojan://public-test-password@host.example:443?type=tcp&security=none#TrojanNone"
	if _, err := clashProxy(trojanNone); err == nil {
		t.Fatal("Trojan without TLS should be marked unsupported")
	}
	body, omitted, err := renderClash([]string{vlessXHTTP, vmessXHTTP})
	if err != nil || len(omitted) != 1 || !strings.Contains(string(body), `"XHTTP"`) || strings.Contains(string(body), `"VMessXHTTP"`) {
		t.Fatal("partial Clash export did not skip unsupported node", err, omitted, string(body))
	}
}

func TestClashHysteria2AndVLESSEncryption(t *testing.T) {
	pin := strings.Repeat("ab", 32)
	hy := "hysteria2://pw@203.0.113.10:443/?sni=203.0.113.10&insecure=1&pinSHA256=" + pin + "&obfs=salamander&obfs-password=o&ports=20000-30000&hop-interval=30&up=30+mbps&down=200+mbps#HY2"
	p, err := clashProxy(hy)
	if err != nil {
		t.Fatal(err)
	}
	if p["type"] != "hysteria2" || p["password"] != "pw" || p["obfs"] != "salamander" || p["obfs-password"] != "o" {
		t.Fatalf("hysteria2 mapping lost: %v", p)
	}
	if p["skip-cert-verify"] != true || p["fingerprint"] != pin || p["ports"] != "20000-30000" || p["hop-interval"] != 30 {
		t.Fatalf("hysteria2 transport options lost: %v", p)
	}
	if p["up"] != "30 mbps" || p["down"] != "200 mbps" {
		t.Fatalf("hysteria2 brutal options lost: %v", p)
	}

	enc := "vless://fbaea75a-e91c-4ad9-9410-7e43aa967766@host.example:443?type=tcp&security=tls&sni=host.example&encryption=mlkem768x25519plus.native.0rtt.KEY#ENC"
	p, err = clashProxy(enc)
	if err != nil {
		t.Fatal(err)
	}
	if p["encryption"] != "mlkem768x25519plus.native.0rtt.KEY" {
		t.Fatalf("vless encryption lost: %v", p)
	}

	for _, link := range []string{
		"vless://fbaea75a-e91c-4ad9-9410-7e43aa967766@host.example:443?type=mkcp&security=tls#KCP",
		"vless://fbaea75a-e91c-4ad9-9410-7e43aa967766@host.example:443?type=hysteria&security=tls#HY",
	} {
		if _, err := clashProxy(link); err == nil {
			t.Fatalf("unsupported VLESS transport should be omitted: %s", link)
		}
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
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pbk := base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes())
	link := strings.Replace(clashRealityFixture("Reality"), base64.RawURLEncoding.EncodeToString(make([]byte, 32)), pbk, 1)
	xhttp := strings.Replace(strings.Replace(link, "type=tcp", "type=xhttp&path=%2Fxhttp", 1), "&flow=xtls-rprx-vision", "", 1)
	httpUpgrade := "vless://fbaea75a-e91c-4ad9-9410-7e43aa967766@host.example:443?type=httpupgrade&security=tls&path=%2Fup&host=cdn.example&sni=host.example#HTTPUpgrade"
	vmessUpgrade := "vmess://fbaea75a-e91c-4ad9-9410-7e43aa967766@host.example:443?type=httpupgrade&security=tls&path=%2Fup&host=cdn.example&sni=host.example#VMessUpgrade"
	trojanUpgrade := "trojan://public-test-password@host.example:443?type=httpupgrade&security=tls&path=%2Fup&host=cdn.example&sni=host.example#TrojanUpgrade"
	for _, links := range [][]string{{link}, {xhttp}, {httpUpgrade}, {vmessUpgrade}, {trojanUpgrade}, {"vless://fbaea75a-e91c-4ad9-9410-7e43aa967766@host.example:443?type=ws&security=tls&path=%2Fws&sni=host.example#WS"}, {"trojan://public-test-password@host.example:443?type=grpc&security=tls&sni=host.example&serviceName=service#RPC"}, {"vmess://fbaea75a-e91c-4ad9-9410-7e43aa967766@host.example:443?security=tls&type=tcp&encryption=auto#VMess"}} {
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
			if len(out) > 1000 {
				out = out[:1000]
			}
			t.Fatalf("pinned Mihomo rejected public fixture: %v %s", err, out)
		}
	}
}
