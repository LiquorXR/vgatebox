package main

import (
	"encoding/json"
	"errors"
	"fanout/internal/paths"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testEgressImage() EgressProfile {
	country, asn, isp := "US", "AS7922", "Comcast"
	no := false
	return EgressProfile{IP: "73.1.1.1", Protocol: "openvpn", Country: &country, ASN: &asn, ISP: &isp,
		IPType: "strict_residential", Hosting: &no, Confidence: "high", Reason: "explicit_residential",
		Provider: "independent_https_fixture", QueriedAt: time.Now().UTC().Format(time.RFC3339Nano),
		ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano), RuleVersion: "residential-2",
		IntelligenceStatus: "passed", Raw: json.RawMessage(`{"ip":"73.1.1.1","fixture":true}`)}
}

func TestOpenVPNEgressRejectsWrongProtocolIPAndStaleEvidence(t *testing.T) {
	tests := []struct {
		name   string
		modify func(*EgressProfile)
	}{
		{"other-ip", func(p *EgressProfile) { p.IP = "8.8.4.4" }},
		{"sstp", func(p *EgressProfile) { p.Protocol = "sstp" }},
		{"expired", func(p *EgressProfile) { p.ExpiresAt = "2000-01-01T00:00:00Z" }},
		{"missing-source", func(p *EgressProfile) { p.Provider = "" }},
		{"failed-residential", func(p *EgressProfile) { p.IntelligenceStatus = "failed" }},
		{"invented-type", func(p *EgressProfile) { p.IPType = "residential" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			image := testEgressImage()
			tc.modify(&image)
			blob, _ := json.Marshal(image)
			got := decodeOpenVPNEgress(blob, "73.1.1.1")
			if got.IPType != "unknown" || got.ISP != nil || got.Raw != nil {
				t.Fatalf("unsafe image accepted: %+v", got)
			}
		})
	}
	blob, _ := json.Marshal(testEgressImage())
	if got := decodeOpenVPNEgress(blob, "73.1.1.1"); got.IPType != "strict_residential" {
		t.Fatalf("valid image lost: %+v", got)
	}
}

func TestOpenVPNEgressProcessFailureDoesNotLeakProviderOutput(t *testing.T) {
	image := lookupOpenVPNEgressWith(t.TempDir(), "73.1.1.1", func(cmd *exec.Cmd) ([]byte, error) {
		return []byte("secret-provider-token"), errors.New("secret-provider-token")
	})
	blob, _ := json.Marshal(image)
	if image.IPType != "unknown" || strings.Contains(string(blob), "secret") {
		t.Fatalf("unsafe failure: %s", blob)
	}
}

// Exercise the actual Python CLI/Go JSON boundary without sockets or a provider.
func TestOpenVPNEgressPythonBridgeWithoutCredentials(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := paths.Ensure(dir); err != nil {
		t.Fatal(err)
	}
	env := filepath.Join(dir, "intelligence.env")
	if err := os.WriteFile(env, []byte("INTELLIGENCE_URL_TEMPLATE=\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FANOUT_TOOLBOX_SCRIPT", filepath.Join(root, "scripts/toolbox.py"))
	t.Setenv("FANOUT_POOL_CONFIG", filepath.Join(root, "config/settings.json"))
	t.Setenv("FANOUT_INTELLIGENCE_ENV_FILE", env)
	t.Setenv("INTELLIGENCE_URL_TEMPLATE", "")
	image := lookupOpenVPNEgressWith(dir, "73.1.1.1", func(cmd *exec.Cmd) ([]byte, error) { return cmd.Output() })
	if image.IPType != "unknown" || image.IntelligenceStatus != "not_configured" || image.IP != "73.1.1.1" {
		t.Fatalf("bridge failed: %+v", image)
	}
	// A synthetic fresh adapter cache exercises positive classification too,
	// with no HTTP request and no production credential.
	template := "https://intelligence.example.com/{ip}"
	t.Setenv("INTELLIGENCE_URL_TEMPLATE", template)
	provider := "independent_https_" + profileDigest(template)[:12]
	cache := map[string]interface{}{provider + ":73.1.1.1": map[string]interface{}{
		"raw": map[string]interface{}{"ip": "73.1.1.1", "country_code": "US",
			"asn":     map[string]interface{}{"asn": "AS7922", "name": "Comcast", "type": "isp"},
			"privacy": map[string]interface{}{"is_hosting": false, "is_residential": true}},
		"queried_at": time.Now().UTC().Format(time.RFC3339Nano),
		"expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano),
	}}
	blob, _ := json.Marshal(cache)
	if err := os.WriteFile(paths.IntelCacheOpenVPN(dir), blob, 0600); err != nil {
		t.Fatal(err)
	}
	image = lookupOpenVPNEgressWith(dir, "73.1.1.1", func(cmd *exec.Cmd) ([]byte, error) { return cmd.Output() })
	if image.IPType != "strict_residential" || !image.CacheUsed || image.Raw == nil || image.Provider != provider {
		t.Fatalf("positive bridge failed: %+v", image)
	}
}

func TestOpenVPNEgressNeedsIndependentSocksMatchAndInvalidatesOnReplacement(t *testing.T) {
	image := testEgressImage()
	tunnel := &Tunnel{Slot: 1, Port: 17928, Node: Node{HostName: "a", Config: "profile-a"}, Status: "up", ExitIP: image.IP}
	if err := tunnel.acceptExitEvidence(image.IP, "8.8.4.4", image); err == nil {
		t.Fatal("accepted mismatched SOCKS")
	}
	if tunnel.egressSnapshot(false) != nil {
		t.Fatal("failed verification published an image")
	}
	if err := tunnel.acceptExitEvidence(image.IP, image.IP, image); err != nil {
		t.Fatal(err)
	}
	if tunnel.egressSnapshot(false).Raw != nil || tunnel.egressSnapshot(true).Raw == nil {
		t.Fatal("raw response visibility wrong")
	}
	tunnel.Node = Node{HostName: "b", Config: "profile-b"}
	if tunnel.egressSnapshot(false) != nil {
		t.Fatal("new node inherited old image")
	}
	if tunnel.Slot != 1 || tunnel.Port != 17928 {
		t.Fatal("slot identity changed")
	}
	tunnel.clearEgress()
	if tunnel.egress != nil || tunnel.egressVerifiedAt != "" {
		t.Fatal("old verification survived clear")
	}
}

func TestOpenVPNEgressExpiresWithoutClaimingResidential(t *testing.T) {
	image := testEgressImage()
	image.ExpiresAt = "2000-01-01T00:00:00Z"
	tunnel := &Tunnel{Status: "up", ExitIP: image.IP}
	if err := tunnel.acceptExitEvidence(image.IP, image.IP, image); err != nil {
		t.Fatal(err)
	}
	got := tunnel.egressSnapshot(true)
	if got.IPType != "unknown" || got.IntelligenceStatus != "stale" || got.Raw == nil || got.QueriedAt == "" {
		t.Fatalf("expiry lost provenance or claimed residential: %+v", got)
	}
	tunnel.Status = "starting"
	if tunnel.egressSnapshot(false) != nil {
		t.Fatal("recovery showed old profile as active")
	}
}

func TestSlotHealthPersistsPrivateIndependentEvidenceWithoutCredentials(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(3, dir)
	image := testEgressImage()
	tunnel := &Tunnel{Slot: 1, Port: 17928, Node: Node{HostName: "a", Config: "profile", PoolNodeID: "node-a"},
		Status: "up", ExitIP: image.IP, Cred: SocksCred{User: "socks-secret-user", Pass: "socks-secret-password"}}
	if err := tunnel.acceptExitEvidence(image.IP, image.IP, image); err != nil {
		t.Fatal(err)
	}
	m.tunnels[1] = tunnel
	if err := m.saveState(); err != nil {
		t.Fatal(err)
	}
	path := paths.SlotHealth(dir)
	blob, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var saved struct{ Slots []slotHealthEvidence }
	if err := json.Unmarshal(blob, &saved); err != nil {
		t.Fatal(err)
	}
	slot := saved.Slots[0]
	if slot.SlotID != "slot-01" || slot.SocksPort != 17928 || slot.ExpectedExitIP != image.IP ||
		slot.ActualExitIP != image.IP || slot.VerifiedAt == "" || slot.ProfileSHA256 != profileDigest("profile") || slot.Egress.Raw == nil {
		t.Fatalf("evidence incomplete: %s", blob)
	}
	if strings.Contains(string(blob), "socks-secret") {
		t.Fatal("credentials in evidence")
	}
	stat, _ := os.Stat(path)
	if stat.Mode().Perm() != 0600 {
		t.Fatalf("private evidence permissions: %v", stat.Mode())
	}
	tunnel.Status = "failed"
	tunnel.clearEgress()
	if err := m.saveState(); err != nil {
		t.Fatal(err)
	}
	blob, _ = os.ReadFile(path)
	saved.Slots = nil
	if err := json.Unmarshal(blob, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Slots[0].Egress != nil || saved.Slots[0].VerifiedAt != "" {
		t.Fatal("failed slot retained successful evidence")
	}
}

func TestSlotHealthUsesSharedDBWhenConsolePresent(t *testing.T) {
	c := consoleFixture(t)
	m := c.m
	image := testEgressImage()
	tunnel := &Tunnel{Slot: 1, Port: 17928, Node: Node{HostName: "a", Config: "profile", PoolNodeID: "node-a"},
		Status: "up", ExitIP: image.IP, Cred: SocksCred{User: "socks-secret-user", Pass: "socks-secret-password"}}
	if err := tunnel.acceptExitEvidence(image.IP, image.IP, image); err != nil {
		t.Fatal(err)
	}
	m.tunnels[1] = tunnel
	if err := m.saveState(); err != nil {
		t.Fatal(err)
	}
	// DB 模式下旧文件必须消失，库里可读回同一快照。
	if _, err := os.Stat(paths.SlotHealth(m.workDir)); !os.IsNotExist(err) {
		t.Fatal("legacy slot_health file retained in DB mode")
	}
	blob, err := m.slotHealthStore().Load()
	if err != nil {
		t.Fatal(err)
	}
	var saved struct{ Slots []slotHealthEvidence }
	if err := json.Unmarshal(blob, &saved); err != nil {
		t.Fatal(err)
	}
	slot := saved.Slots[0]
	if slot.SlotID != "slot-01" || slot.SocksPort != 17928 || slot.ExpectedExitIP != image.IP ||
		slot.ActualExitIP != image.IP || slot.VerifiedAt == "" || slot.ProfileSHA256 != profileDigest("profile") || slot.Egress.Raw == nil {
		t.Fatalf("evidence incomplete: %s", blob)
	}
	if strings.Contains(string(blob), "socks-secret") {
		t.Fatal("credentials in evidence")
	}
}

func TestValidatedPoolKeepsSourceLabelAwayFromEgressType(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "profile.ovpn"), []byte("client"), 0600); err != nil {
		t.Fatal(err)
	}
	pool := validatedPoolFile{Nodes: []validatedPoolNode{
		{Hostname: "vpn-home", ServerIP: "70.79.253.35", AdvertisedCountry: "JP"},
		{Hostname: "public-vpn-2", ServerIP: "219.100.37.5", AdvertisedCountry: "JP"},
	}}
	for i := range pool.Nodes {
		pool.Nodes[i].Protocols.OpenVPN.ProfileRef = "profile.ovpn"
	}
	path := filepath.Join(dir, "node_pool.json")
	blob, _ := json.Marshal(pool)
	if err := os.WriteFile(path, blob, 0600); err != nil {
		t.Fatal(err)
	}
	nodes, err := fetchNodesFromValidatedPool(path)
	if err != nil {
		t.Fatal(err)
	}
	byHost := map[string]Node{}
	for _, n := range nodes {
		byHost[n.HostName] = n
	}
	// 没有 OpenVPN 画像时，来源仍是志愿者，但出口类型是 unknown，不能反过来说成自营。
	if !byHost["vpn-home"].SourceResidential || byHost["vpn-home"].Residential {
		t.Fatalf("volunteer source was conflated with egress type: %+v", byHost["vpn-home"])
	}
	if byHost["public-vpn-2"].SourceResidential || byHost["public-vpn-2"].Residential {
		t.Fatalf("self-operated source was mislabeled: %+v", byHost["public-vpn-2"])
	}
}

func TestValidatedPoolDoesNotBorrowSSTPImage(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "profile.ovpn"), []byte("client"), 0600); err != nil {
		t.Fatal(err)
	}
	pool := validatedPoolFile{Nodes: []validatedPoolNode{{Hostname: "candidate", AdvertisedCountry: "JP"}}}
	item := &pool.Nodes[0]
	item.Protocols.OpenVPN.ProfileRef = "profile.ovpn"
	item.Protocols.SSTP.Status = "passed"
	item.Protocols.SSTP.ExpectedExitIP = "73.1.1.1"
	item.Protocols.SSTP.Egress.IPType = "strict_residential"
	item.Protocols.SSTP.Egress.ISP = "SSTP ISP"
	item.Protocols.SSTP.Egress.ASN = "AS7922"
	path := filepath.Join(dir, "node_pool.json")
	load := func() Node {
		t.Helper()
		blob, _ := json.Marshal(pool)
		if err := os.WriteFile(path, blob, 0600); err != nil {
			t.Fatal(err)
		}
		nodes, err := fetchNodesFromValidatedPool(path)
		if err != nil {
			t.Fatal(err)
		}
		return nodes[0]
	}
	got := load()
	if got.IPType != "unknown" || got.ISP != "" || got.ASN != "" || got.ExpectedExitIP != "" || got.Residential {
		t.Fatalf("SSTP leaked into OpenVPN candidate: %+v", got)
	}
	item.Protocols.OpenVPN.Status = "passed"
	item.Protocols.OpenVPN.ExpectedExitIP = "73.1.1.1"
	item.Protocols.OpenVPN.Egress = testEgressImage()
	got = load()
	if got.IPType != "strict_residential" || got.ISP != "Comcast" || !got.Residential {
		t.Fatalf("fresh OpenVPN image lost: %+v", got)
	}
	for _, mutate := range []func(){
		func() { item.Protocols.OpenVPN.Egress.ExpiresAt = "2000-01-01T00:00:00Z" },
		func() {
			item.Protocols.OpenVPN.Egress = testEgressImage()
			item.Protocols.OpenVPN.ExpectedExitIP = "8.8.4.4"
		},
	} {
		mutate()
		if got := load(); got.IPType != "unknown" || got.Residential {
			t.Fatalf("invalid OpenVPN evidence used: %+v", got)
		}
	}
}
