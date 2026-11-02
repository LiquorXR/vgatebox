package main

import "testing"

func TestNormalizeInboundSpecDefaults(t *testing.T) {
	ns, err := normalizeInboundSpec(NewInboundSpec{Port: 12345}, map[int]bool{})
	if err != nil {
		t.Fatalf("意外报错: %v", err)
	}
	if ns.Protocol != "vless" || ns.Network != "tcp" || ns.Security != "none" {
		t.Fatalf("默认值不对: %+v", ns)
	}
	if ns.Remark != "vless-12345" {
		t.Errorf("备注 = %q, want vless-12345", ns.Remark)
	}
	if ns.Path != "" {
		t.Errorf("tcp 不该生成路径, got %q", ns.Path)
	}
}

func TestNormalizeInboundSpecGeneratesPath(t *testing.T) {
	for _, net := range []string{"ws", "httpupgrade", "xhttp", "grpc"} {
		ns, err := normalizeInboundSpec(NewInboundSpec{Network: net, Port: 1234}, map[int]bool{})
		if err != nil {
			t.Fatalf("%s: %v", net, err)
		}
		if ns.Path == "" {
			t.Errorf("%s 应自动生成路径", net)
		}
	}
}

func TestNormalizeInboundSpecRejects(t *testing.T) {
	cases := []struct {
		name string
		spec NewInboundSpec
		used map[int]bool
	}{
		{"未知协议", NewInboundSpec{Protocol: "ss", Port: 1}, nil},
		{"VMess 已移除", NewInboundSpec{Protocol: "vmess", Port: 1}, nil},
		{"Trojan 已移除", NewInboundSpec{Protocol: "trojan", Port: 1}, nil},
		{"未知传输", NewInboundSpec{Network: "quic", Port: 1}, nil},
		{"未知安全层", NewInboundSpec{Security: "xtls", Port: 1}, nil},
		{"REALITY 配 ws", NewInboundSpec{Network: "ws", Security: "reality", Port: 1}, nil},
		{"端口占用", NewInboundSpec{Port: 443}, map[int]bool{443: true}},
		{"vision 配 ws", NewInboundSpec{Network: "ws", Security: "tls", Vision: true, Port: 1}, nil},
	}
	for _, c := range cases {
		if _, err := normalizeInboundSpec(c.spec, c.used); err == nil {
			t.Errorf("%s: 应当报错但通过了", c.name)
		}
	}
}

func TestNormalizeInboundSpecVision(t *testing.T) {
	ns, err := normalizeInboundSpec(NewInboundSpec{
		Protocol: "vless", Network: "tcp", Security: "reality", Vision: true, Port: 1234,
	}, nil)
	if err != nil {
		t.Fatalf("意外报错: %v", err)
	}
	if ns.Flow != "xtls-rprx-vision" {
		t.Errorf("Flow = %q, want xtls-rprx-vision", ns.Flow)
	}
}

func TestNormalizeHysteria2(t *testing.T) {
	ns, err := normalizeInboundSpec(NewInboundSpec{
		Protocol: "hysteria", HysteriaAuth: "secret",
	}, nil)
	if err != nil {
		t.Fatalf("意外报错: %v", err)
	}
	if ns.Protocol != "hysteria" || ns.Network != "hysteria" || ns.Security != "tls" {
		t.Fatalf("Hysteria2 默认组合不对: %+v", ns)
	}
	if ns.Hysteria == nil || ns.Hysteria.Auth != "secret" {
		t.Fatalf("Hysteria2 配置丢失: %+v", ns.Hysteria)
	}
}

func TestNormalizeHysteriaPortRange(t *testing.T) {
	ns, err := normalizeInboundSpec(NewInboundSpec{
		Protocol: "hysteria", HysteriaPorts: "20000-20005",
	}, nil)
	if err != nil {
		t.Fatalf("意外报错: %v", err)
	}
	if ns.Port < 20000 || ns.Port > 20005 || ns.Hysteria.Ports != "20000-20005" {
		t.Fatalf("端口范围未生效: %+v", ns)
	}
	for _, spec := range []NewInboundSpec{
		{Protocol: "hysteria", Port: 31000, HysteriaPorts: "20000-20005"},
		{Protocol: "hysteria", HysteriaPorts: "20000,20001"},
		{Protocol: "hysteria", HysteriaPorts: "30000-20000"},
	} {
		if _, err := normalizeInboundSpec(spec, nil); err == nil {
			t.Fatalf("应拒绝端口范围: %+v", spec)
		}
	}
}

func TestNormalizeHysteria2Rejects(t *testing.T) {
	for _, spec := range []NewInboundSpec{
		{Protocol: "hysteria", Network: "xhttp", Security: "tls", Port: 1},
		{Protocol: "hysteria", Network: "hysteria", Security: "none", Port: 1},
		{Protocol: "hysteria", Network: "hysteria", Security: "reality", Port: 1},
		{Protocol: "vless", Network: "hysteria", Security: "none", Port: 1},
	} {
		if _, err := normalizeInboundSpec(spec, nil); err == nil {
			t.Fatalf("应当拒绝: %+v", spec)
		}
	}
}

func TestNormalizeVLESSEncryption(t *testing.T) {
	ns, err := normalizeInboundSpec(NewInboundSpec{
		Protocol: "vless", Network: "xhttp", Security: "none", Port: 1,
		VLESSEncryption: true, VLESSEncryptionType: "mlkem768",
		Vision: true,
	}, nil)
	if err != nil {
		t.Fatalf("意外报错: %v", err)
	}
	if ns.VLESSEncryption == nil || ns.VLESSEncryption.KeyType != "mlkem768" {
		t.Fatalf("VLESS Encryption 配置丢失: %+v", ns.VLESSEncryption)
	}
	// 启用 VLESS Encryption 后，上游允许 Vision 跨传输。
	if ns.Flow != "xtls-rprx-vision" {
		t.Fatalf("Flow = %q", ns.Flow)
	}
	if _, err := normalizeInboundSpec(NewInboundSpec{
		Protocol: "vless", Port: 1, VLESSEncryption: true, VLESSEncryptionType: "bad",
	}, nil); err == nil {
		t.Fatal("非法密钥类型应被拒绝")
	}
}
