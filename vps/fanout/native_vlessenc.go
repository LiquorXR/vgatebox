package main

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// newVLESSEncryption 生成一对 VLESS Encryption 参数。
//
// 握手固定为 mlkem768x25519plus；最后一段认证参数可以来自 X25519 或
// ML-KEM-768。服务端使用 decryption，客户端使用 encryption，两者必须成对。
func newVLESSEncryption(xrayBin, keyType string) (*vlessEncryptionConfig, error) {
	sub := ""
	switch strings.ToLower(strings.TrimSpace(keyType)) {
	case "", "x25519", "native":
		keyType = "x25519"
		sub = "x25519"
	case "mlkem768", "ml-kem-768", "mlkem":
		keyType = "mlkem768"
		sub = "mlkem768"
	default:
		return nil, fmt.Errorf("不支持的 VLESS Encryption 密钥类型 %q", keyType)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, xrayBin, sub)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("生成 VLESS Encryption 参数失败: %w", err)
	}

	server, client := "", ""
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "PrivateKey:"):
			server = strings.TrimSpace(strings.TrimPrefix(line, "PrivateKey:"))
		case strings.HasPrefix(line, "Password (PublicKey):"):
			client = strings.TrimSpace(strings.TrimPrefix(line, "Password (PublicKey):"))
		case strings.HasPrefix(line, "Seed:"):
			server = strings.TrimSpace(strings.TrimPrefix(line, "Seed:"))
		case strings.HasPrefix(line, "Client:"):
			client = strings.TrimSpace(strings.TrimPrefix(line, "Client:"))
		}
	}
	if server == "" || client == "" {
		return nil, fmt.Errorf("VLESS Encryption 参数输出不完整")
	}

	return &vlessEncryptionConfig{
		Enabled:          true,
		KeyType:          keyType,
		ServerDecryption: "mlkem768x25519plus.native.600s." + server,
		ClientEncryption: "mlkem768x25519plus.native.0rtt." + client,
	}, nil
}
