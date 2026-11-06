package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// P2-6 fuzz：四个纯函数，无网络、无磁盘、无 root 依赖。
// 平时 `go test` 只跑种子语料；延长跑：
//   go test -fuzz=FuzzParseNodeCSV -fuzztime=5m .
//   go test -fuzz=FuzzClashProxy -fuzztime=5m .
//   go test -fuzz=FuzzNormalizePolicy -fuzztime=5m .
//   go test -fuzz=FuzzNormalizeInboundSpec -fuzztime=5m .

func FuzzParseNodeCSV(f *testing.F) {
	f.Add(sampleNodeCSV("fuzz-seed"))
	f.Add("")
	f.Add("*vpn_servers\n")
	f.Add("<html>劫持页</html>")
	f.Fuzz(func(t *testing.T, body string) {
		nodes, err := parseNodeCSV(body)
		if err != nil {
			return
		}
		for _, n := range nodes {
			// 空 hostname 的行在解析时已跳过，成功结果里不应出现。
			if n.HostName == "" {
				t.Fatalf("成功结果含空 hostname: %+v", n)
			}
		}
	})
}

func FuzzClashProxy(f *testing.F) {
	f.Add("vless://fbaea75a-e91c-4ad9-9410-7e43aa967766@203.0.113.1:443?type=tcp&security=reality&flow=xtls-rprx-vision&sni=example.com&fp=chrome&pbk=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA&sid=12345678#E2E")
	f.Add("vless://fbaea75a-e91c-4ad9-9410-7e43aa967766@host.example:443?type=ws&security=tls&path=%2Fws&host=cdn.example#pin")
	f.Add("")
	f.Add("http://not-vless/")
	f.Fuzz(func(t *testing.T, link string) {
		m, err := clashProxy(link)
		if err != nil {
			return
		}
		for _, k := range []string{"name", "type", "server", "port", "uuid"} {
			if _, ok := m[k]; !ok {
				t.Fatalf("成功结果缺键 %q: %v", k, m)
			}
		}
		if m["type"] != "vless" {
			t.Fatalf("type = %v，想要 vless", m["type"])
		}
		if m["server"] == "" || m["uuid"] == "" {
			t.Fatalf("server/uuid 不应为空: %v", m)
		}
		port, ok := m["port"].(int)
		if !ok || port < 1 || port > 65535 {
			t.Fatalf("port = %v，想要 1..65535 的 int", m["port"])
		}
	})
}

func FuzzNormalizePolicy(f *testing.F) {
	strict := defaultSlotPolicyForMode("JP", true)
	strict.PreferredISP = []string{"Example ISP"}
	strict.AllowedASN = []string{"AS12345"}
	strict.MaximumLatencyMS = 1500
	f.Add(mustMarshalPolicyT(f, defaultSlotPolicyForMode("", false)))
	f.Add(mustMarshalPolicyT(f, strict))
	f.Add(`{}`)
	f.Add(`null`)
	f.Add(``)
	f.Fuzz(func(t *testing.T, data string) {
		var p SlotPolicy
		if err := json.Unmarshal([]byte(data), &p); err != nil {
			return
		}
		out, err := normalizePolicy(p)
		if err != nil {
			return
		}
		if out.Country != "" && !policyCountry.MatchString(out.Country) {
			t.Fatalf("country = %q", out.Country)
		}
		if out.MaximumLatencyMS < 0 || out.MaximumLatencyMS > 120000 {
			t.Fatalf("latency = %d", out.MaximumLatencyMS)
		}
		if len(out.AllowedIPTypes) == 0 {
			t.Fatal("成功结果的允许类型为空")
		}
		for _, typ := range out.AllowedIPTypes {
			if typ != "strict_residential" && typ != "unknown" {
				t.Fatalf("未知归一化类型 %q", typ)
			}
		}
		for _, list := range [][]string{out.PreferredISP, out.AllowedISP, out.BlockedISP, out.AllowedASN, out.BlockedASN} {
			if len(list) > 100 {
				t.Fatalf("列表超长 %d", len(list))
			}
			for _, item := range list {
				if item == "" || len(item) > 200 || strings.ContainsAny(item, "\r\n\x00") {
					t.Fatalf("列表含非法值 %q", item)
				}
			}
		}
		for _, asn := range append(append([]string{}, out.AllowedASN...), out.BlockedASN...) {
			if !policyASN.MatchString(asn) {
				t.Fatalf("ASN 形状不对 %q", asn)
			}
		}
	})
}

func FuzzNormalizeInboundSpec(f *testing.F) {
	valid, err := json.Marshal(NewInboundSpec{Protocol: "vless", Network: "tcp", Port: 31000, Security: "reality", Dest: "www.tesla.com:443", ServerNames: "www.tesla.com"})
	if err != nil {
		panic(err)
	}
	f.Add(string(valid))
	f.Add(`{}`)
	f.Add(`{"protocol":"vmess"}`)
	f.Fuzz(func(t *testing.T, data string) {
		var spec NewInboundSpec
		if err := json.Unmarshal([]byte(data), &spec); err != nil {
			return
		}
		if spec.Port == 0 {
			// 0 会触发真实端口探测/绑定：fuzz 只测校验与归一，不碰套接字。
			spec.Port = 31000
		}
		out, err := normalizeInboundSpec(spec, map[int]bool{})
		if err != nil {
			return
		}
		if out.Protocol != "vless" {
			t.Fatalf("protocol = %q", out.Protocol)
		}
		if out.Port != spec.Port {
			t.Fatalf("port = %d，想要输入的 %d", out.Port, spec.Port)
		}
		if out.Network == "" || out.Remark == "" {
			t.Fatalf("network/remark 不应为空: %+v", out)
		}
		if enc := out.VLESSEncryption; enc != nil && enc.KeyType != "x25519" && enc.KeyType != "mlkem768" {
			t.Fatalf("加密类型 = %q", enc.KeyType)
		}
		if out.Flow != "" && out.Flow != "xtls-rprx-vision" {
			t.Fatalf("flow = %q", out.Flow)
		}
	})
}

func mustMarshalPolicyT(f *testing.F, p SlotPolicy) string {
	f.Helper()
	blob, err := json.Marshal(p)
	if err != nil {
		f.Fatal(err)
	}
	return string(blob)
}
