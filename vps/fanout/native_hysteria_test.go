package main

import (
	"encoding/json"
	"net"
	"strings"
	"testing"
)

func TestUDPPortAvailability(t *testing.T) {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	port := conn.LocalAddr().(*net.UDPAddr).Port
	if portAvailableUDP(port) {
		t.Fatalf("UDP port %d reported free while bound", port)
	}
}

func TestHysteriaInboundJSON(t *testing.T) {
	ib := &nativeInbound{
		ID: 1, Port: 443, Protocol: "hysteria", Network: "hysteria", Security: "tls",
		TLS: &tlsConfig{ServerName: "203.0.113.10", CertFile: "/cert", KeyFile: "/key"},
		Hysteria: &hysteriaConfig{
			Auth: "auth-pass", ObfsPassword: "obfs-pass", Ports: "20000-30000", HopInterval: 30,
			Up: "30 mbps", Down: "200 mbps", UDPIdleTimeout: 60,
		},
		Clients: []nativeClient{{Email: "u", Password: "auth-pass", Enable: true}},
	}
	raw := nativeInboundJSON(ib)
	settings := raw["settings"].(map[string]any)
	if settings["version"] != 2 {
		t.Fatalf("hysteria version lost: %v", settings)
	}
	clients := settings["clients"].([]any)
	if len(clients) != 1 || clients[0].(map[string]any)["auth"] != "auth-pass" {
		t.Fatalf("hysteria clients lost: %v", clients)
	}
	if _, old := settings["users"]; old {
		t.Fatalf("hysteria server must use clients, not users: %v", settings)
	}
	st := raw["streamSettings"].(map[string]any)
	if st["security"] != "tls" {
		t.Fatalf("hysteria must use tls: %v", st)
	}
	tls := st["tlsSettings"].(map[string]any)
	alpn, ok := tls["alpn"].([]any)
	if !ok || len(alpn) != 1 || alpn[0] != "h3" {
		t.Fatalf("hysteria tls alpn must be h3: %v", tls)
	}
	hs := st["hysteriaSettings"].(map[string]any)
	if hs["auth"] != "auth-pass" || hs["udpIdleTimeout"] != 60 {
		t.Fatalf("hysteria settings lost: %v", hs)
	}
	fm := st["finalmask"].(map[string]any)
	udp := fm["udp"].([]any)
	if udp[0].(map[string]any)["type"] != "salamander" {
		t.Fatalf("salamander lost: %v", fm)
	}
	qp := fm["quicParams"].(map[string]any)
	if qp["congestion"] != "brutal" || qp["brutalUp"] != "30 mbps" || qp["brutalDown"] != "200 mbps" {
		t.Fatalf("brutal settings lost: %v", qp)
	}
}

func TestVLESSEncryptionConfigAndLink(t *testing.T) {
	ib := &nativeInbound{
		ID: 1, Port: 8443, Protocol: "vless", Network: "xhttp", Security: "none", Path: "/x",
		VLESSEncryption: &vlessEncryptionConfig{
			Enabled: true, KeyType: "mlkem768",
			ServerDecryption: "mlkem768x25519plus.native.600s.SERVER",
			ClientEncryption: "mlkem768x25519plus.native.0rtt.CLIENT",
		},
		Clients: []nativeClient{{ID: "uuid", Email: "u", Enable: true}},
	}
	raw := nativeInboundJSON(ib)
	settings := raw["settings"].(map[string]any)
	if settings["decryption"] != ib.VLESSEncryption.ServerDecryption {
		t.Fatalf("vless decryption lost: %v", settings)
	}
	link := shareLink(ib, ib.Clients[0], "203.0.113.10")
	if !strings.Contains(link, "encryption=mlkem768x25519plus.native.0rtt.CLIENT") {
		t.Fatalf("vless encryption missing from link: %s", link)
	}
}

func TestHysteriaServerPortRangeConfig(t *testing.T) {
	ib := &nativeInbound{
		Port: 20000, Protocol: "hysteria", Network: "hysteria", Security: "tls",
		TLS:      &tlsConfig{ServerName: "203.0.113.10", CertFile: "/cert", KeyFile: "/key"},
		Hysteria: &hysteriaConfig{Auth: "auth-pass", Ports: "20000-30000", HopInterval: 30},
		Clients:  []nativeClient{{Email: "u", Password: "auth-pass", Enable: true}},
	}
	raw := nativeInboundJSON(ib)
	if got, ok := raw["port"].(string); !ok || got != "20000-30000" {
		t.Fatalf("hysteria server port range lost: %#v", raw["port"])
	}
}

func TestVLESSOverHysteriaHasNoClientLink(t *testing.T) {
	ib := &nativeInbound{
		Port: 443, Protocol: "vless", Network: "hysteria", Security: "tls",
		TLS: &tlsConfig{ServerName: "203.0.113.10"}, Hysteria: &hysteriaConfig{Auth: "a"},
		Clients: []nativeClient{{ID: "uuid", Email: "u", Enable: true}},
	}
	if link := shareLink(ib, ib.Clients[0], "203.0.113.10"); link != "" {
		t.Fatalf("VLESS over Hysteria must not produce a standard client link: %s", link)
	}
}

func TestHysteriaMbps(t *testing.T) {
	for raw, want := range map[string]string{
		"30 mbps": "30", "200 mbps": "200", "1 gbps": "1000",
		"500 kbps": "0.5", "30": "30", "bad": "",
	} {
		if got := hysteriaMbps(raw); got != want {
			t.Fatalf("hysteriaMbps(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestDecodeNativeStoreDropsLegacyProtocols(t *testing.T) {
	blob := []byte(`{
	  "next_id": 3,
	  "inbounds": [
	    {"id":1,"port":1001,"protocol":"vmess","network":"ws","security":"tls","clients":[{"id":"u","email":"e","enable":true}]},
	    {"id":2,"port":1002,"protocol":"trojan","network":"tcp","security":"tls","clients":[{"password":"p","email":"e","enable":true}]}
	  ]
	}`)
	st, err := decodeNativeStore(blob)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Inbounds) != 0 || !st.droppedLegacy {
		t.Fatalf("legacy protocols were not filtered: %+v", st)
	}
	if _, err := json.Marshal(st); err != nil {
		t.Fatal(err)
	}
}
