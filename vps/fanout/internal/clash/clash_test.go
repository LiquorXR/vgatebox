package clash

import "testing"

func TestProxyVLESSReality(t *testing.T) {
	m, err := Proxy("vless://fbaea75a-e91c-4ad9-9410-7e43aa967766@203.0.113.1:443?type=tcp&security=reality&flow=xtls-rprx-vision&sni=example.com&fp=chrome&pbk=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA&sid=12345678#E2E")
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"name", "type", "server", "port", "uuid"} {
		if _, ok := m[k]; !ok {
			t.Fatalf("missing key %q", k)
		}
	}
	if m["type"] != "vless" {
		t.Fatalf("type = %v", m["type"])
	}
}

func TestProxyRejectsMKCPAsUnsupported(t *testing.T) {
	_, err := Proxy("vless://u@h:1?type=kcp&security=none#x")
	if !IsUnsupported(err) {
		t.Fatalf("mKCP must be unsupported, got %v", err)
	}
}

func TestRenderOmitsMKCPKeepsVLESS(t *testing.T) {
	body, omitted, err := Render([]string{
		"vless://u@h:1?type=tcp&security=none#keep",
		"vless://u@h:2?type=kcp&security=none#drop",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(omitted) != 1 || len(body) == 0 {
		t.Fatalf("omitted=%v body=%d", omitted, len(body))
	}
}
