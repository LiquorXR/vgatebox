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
	proto, network, security, err := normalizeSpecNames(spec)
	if err != nil {
		return nil, err
	}
	port, err := resolveInboundPort(network, spec.Port, used)
	if err != nil {
		return nil, err
	}
	enc, err := normalizeVLESSEncryption(spec, proto)
	if err != nil {
		return nil, err
	}
	flow, err := normalizeVisionFlow(spec, proto, network, security, enc != nil)
	if err != nil {
		return nil, err
	}
	return &normalizedSpec{
		Protocol:        proto,
		Network:         network,
		Security:        security,
		Port:            port,
		Path:            defaultInboundPath(network, spec.Path),
		Host:            strings.TrimSpace(spec.Host),
		Remark:          defaultInboundRemark(spec.Remark, proto, port),
		Flow:            flow,
		KCP:             inboundKCPConfig(spec, network),
		VLESSEncryption: enc,
	}, nil
}

func normalizeSpecNames(spec NewInboundSpec) (string, string, string, error) {
	proto := strings.ToLower(strings.TrimSpace(spec.Protocol))
	if proto == "" {
		proto = "vless"
	}
	if !nativeProtocols[proto] {
		return "", "", "", fmt.Errorf("不支持的协议 %q", spec.Protocol)
	}
	network := strings.ToLower(strings.TrimSpace(spec.Network))
	if network == "" {
		network = "tcp"
	}
	if !nativeNetworks[network] {
		return "", "", "", fmt.Errorf("不支持的传输方式 %q", spec.Network)
	}
	security := strings.ToLower(strings.TrimSpace(spec.Security))
	if security == "" {
		security = "none"
	}
	if !nativeSecurities[security] {
		return "", "", "", fmt.Errorf("不支持的安全层 %q", spec.Security)
	}
	if proto == "vless" && security == "reality" && !realityCapableWith(network) {
		return "", "", "", fmt.Errorf("REALITY 不支持 %s 传输", network)
	}
	return proto, network, security, nil
}

func realityCapableWith(network string) bool {
	return network == "tcp" || network == "xhttp" || network == "grpc"
}

func resolveInboundPort(network string, requested int, used map[int]bool) (int, error) {
	if requested == 0 {
		return freeRandomPortFor(network, used)
	}
	if used[requested] {
		return 0, fmt.Errorf("端口 %d 已被别的入站占用", requested)
	}
	if network == "mkcp" && !portAvailableUDP(requested) {
		return 0, fmt.Errorf("UDP 端口 %d 已被占用", requested)
	}
	return requested, nil
}

func defaultInboundPath(network, raw string) string {
	path := strings.TrimSpace(raw)
	if path != "" {
		return path
	}
	switch network {
	case "ws", "httpupgrade", "xhttp":
		return "/" + randomHex(6)
	case "grpc":
		return randomHex(6)
	default:
		return ""
	}
}

func defaultInboundRemark(raw, proto string, port int) string {
	remark := strings.TrimSpace(raw)
	if remark != "" {
		return remark
	}
	return fmt.Sprintf("%s-%d", proto, port)
}

func normalizeVLESSEncryption(spec NewInboundSpec, proto string) (*vlessEncryptionConfig, error) {
	if !spec.VLESSEncryption {
		return nil, nil
	}
	enc := &vlessEncryptionConfig{
		Enabled: true,
		KeyType: strings.ToLower(strings.TrimSpace(spec.VLESSEncryptionType)),
	}
	if enc.KeyType == "" {
		enc.KeyType = "x25519"
	}
	if enc.KeyType != "x25519" && enc.KeyType != "mlkem768" {
		return nil, fmt.Errorf("VLESS Encryption 密钥类型无效")
	}
	if proto != "vless" {
		return nil, fmt.Errorf("只有 VLESS 支持 VLESS Encryption")
	}
	return enc, nil
}

func normalizeVisionFlow(spec NewInboundSpec, proto, network, security string, encrypted bool) (string, error) {
	if !spec.Vision {
		return "", nil
	}
	if !visionCapableWith(proto, network, security, encrypted) {
		return "", fmt.Errorf("xtls-rprx-vision 只能用于 VLESS + TCP + TLS/REALITY，或启用 VLESS Encryption")
	}
	return "xtls-rprx-vision", nil
}

func inboundKCPConfig(spec NewInboundSpec, network string) *kcpConfig {
	if network != "mkcp" {
		return nil
	}
	return &kcpConfig{
		MTU:      clampIntDefault(spec.KCPMTU, 1350, 576, 1460),
		TTI:      clampIntDefault(spec.KCPTTI, 50, 10, 100),
		Uplink:   clampIntDefault(spec.KCPUplink, 5, 0, 100000),
		Downlink: clampIntDefault(spec.KCPDownlink, 20, 0, 100000),
	}
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
