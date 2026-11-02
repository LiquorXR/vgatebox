package main

import (
	"fmt"
	"strings"
)

// normalizedSpec 是 NewInboundSpec 过完校验、补完默认值之后的样子。
type normalizedSpec struct {
	Protocol string
	Network  string
	Security string
	Port     int
	Path     string
	Host     string
	Remark   string
	Flow     string
	KCP      *kcpConfig
	// VLESSEncryption 为 nil 表示关闭。启用时由 prepareInbound 补齐密钥对。
	VLESSEncryption *vlessEncryptionConfig
}

// normalizeInboundSpec 校验协议组合并补上默认值。
//
// used 是已被占用的端口集合；端口留空时从中避开随机挑一个。
// 这段逻辑从 Native.CreateInbound 里抽出来，便于其他调用方复用同一套校验。
func normalizeInboundSpec(spec NewInboundSpec, used map[int]bool) (*normalizedSpec, error) {
	proto := strings.ToLower(strings.TrimSpace(spec.Protocol))
	if proto == "" {
		proto = "vless"
	}
	if !nativeProtocols[proto] {
		return nil, fmt.Errorf("不支持的协议 %q", spec.Protocol)
	}
	network := strings.ToLower(strings.TrimSpace(spec.Network))
	if network == "" {
		network = "tcp"
	}
	if !nativeNetworks[network] {
		return nil, fmt.Errorf("不支持的传输方式 %q", spec.Network)
	}
	security := strings.ToLower(strings.TrimSpace(spec.Security))
	if security == "" {
		security = "none"
	}
	if !nativeSecurities[security] {
		return nil, fmt.Errorf("不支持的安全层 %q", spec.Security)
	}

	if proto == "vless" {
		// REALITY 靠模仿 TLS 握手工作，套在 ws/grpc 等已有自己头部的传输上没有意义。
		if security == "reality" && network != "tcp" && network != "xhttp" && network != "grpc" {
			return nil, fmt.Errorf("REALITY 不支持 %s 传输", network)
		}
	}

	port := spec.Port
	if port == 0 {
		p, err := freeRandomPortFor(network, used)
		if err != nil {
			return nil, err
		}
		port = p
	} else if used[port] {
		return nil, fmt.Errorf("端口 %d 已被别的入站占用", port)
	} else if network == "mkcp" && !portAvailableUDP(port) {
		return nil, fmt.Errorf("UDP 端口 %d 已被占用", port)
	}

	path := strings.TrimSpace(spec.Path)
	if path == "" {
		switch network {
		case "ws", "httpupgrade", "xhttp":
			path = "/" + randomHex(6)
		case "grpc":
			path = randomHex(6)
		}
	}

	remark := strings.TrimSpace(spec.Remark)
	if remark == "" {
		remark = fmt.Sprintf("%s-%d", proto, port)
	}

	enc := &vlessEncryptionConfig{
		Enabled: spec.VLESSEncryption,
		KeyType: strings.ToLower(strings.TrimSpace(spec.VLESSEncryptionType)),
	}
	if !enc.Enabled {
		enc = nil
	} else {
		if enc.KeyType == "" {
			enc.KeyType = "x25519"
		}
		if enc.KeyType != "x25519" && enc.KeyType != "mlkem768" {
			return nil, fmt.Errorf("VLESS Encryption 密钥类型无效")
		}
		if proto != "vless" {
			return nil, fmt.Errorf("只有 VLESS 支持 VLESS Encryption")
		}
	}

	flow := ""
	if spec.Vision {
		if !visionCapableWith(proto, network, security, enc != nil) {
			return nil, fmt.Errorf("xtls-rprx-vision 只能用于 VLESS + TCP + TLS/REALITY，或启用 VLESS Encryption")
		}
		flow = "xtls-rprx-vision"
	}

	ns := &normalizedSpec{
		Protocol:        proto,
		Network:         network,
		Security:        security,
		Port:            port,
		Path:            path,
		Host:            strings.TrimSpace(spec.Host),
		Remark:          remark,
		Flow:            flow,
		VLESSEncryption: enc,
	}

	switch network {
	case "mkcp":
		ns.KCP = &kcpConfig{
			MTU:      clampIntDefault(spec.KCPMTU, 1350, 576, 1460),
			TTI:      clampIntDefault(spec.KCPTTI, 50, 10, 100),
			Uplink:   clampIntDefault(spec.KCPUplink, 5, 0, 100000),
			Downlink: clampIntDefault(spec.KCPDownlink, 20, 0, 100000),
		}
	}

	return ns, nil
}

func clampIntDefault(v, def, min, max int) int {
	if v == 0 {
		return def
	}
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
