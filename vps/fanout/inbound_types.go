package main

import (
	"strings"
)

// Inbound is the panel-independent view of one Xray inbound.
type Inbound struct {
	ManagedSlot bool   `json:"managed_slot,omitempty"`
	Manual      bool   `json:"manual,omitempty"`
	HostDirect  bool   `json:"host_direct,omitempty"`
	ID          int    `json:"id"`
	Port        int    `json:"port"`
	Protocol    string `json:"protocol"`
	Remark      string `json:"remark"`
	Enable      bool   `json:"enable"`
	Tag         string `json:"tag"`
	BoundTo     string `json:"bound_to"` // bound node hostname; empty means unbound
	BoundSlot   int    `json:"bound_slot,omitempty"`
	NameMode    string `json:"name_mode,omitempty"`
	BoundUp     bool   `json:"bound_up"`
}

// InboundDetail is one inbound plus its clients and generated links.
type InboundDetail struct {
	Inbound
	Clients []ClientInfo `json:"clients"`
	Links   []string     `json:"links"`
	Listen  string       `json:"listen"`
	Network string       `json:"network"`
	TLS     string       `json:"tls"`
	// VLESS Encryption 只暴露开关和密钥类型，服务端 decryption 不外发。
	VLESSEncryption     bool   `json:"vless_encryption,omitempty"`
	VLESSEncryptionType string `json:"vless_encryption_type,omitempty"`
}

type ClientInfo struct {
	Email  string `json:"email"`
	ID     string `json:"id"`
	Enable bool   `json:"enable"`
}

// sanitizeTag turns a hostname into a safe tag fragment.
func sanitizeTag(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	if b.Len() == 0 {
		return "unknown"
	}
	return b.String()
}

// exitLabel is the human-readable label used for generated native inbounds.
func exitLabel(t *Tunnel) string {
	place := nodeLabel(t.Node)

	suffix := t.Node.HostName
	if t.ExitIP != "" {
		if i := strings.LastIndex(t.ExitIP, "."); i >= 0 {
			suffix = t.ExitIP[i+1:]
		} else {
			suffix = t.ExitIP
		}
	}

	if place == "" {
		return suffix
	}
	// Put the distinguishing suffix at the end so names remain unique even
	// when several exits share a country.
	return place + " " + suffix
}

func renameExitLabel(remark, newLabel string) string {
	if newLabel == "" || !isGeneratedLabel(remark) {
		return remark
	}
	return newLabel
}
