package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func publicOriginRequest(remote, host string, headers map[string]string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "http://"+host+"/sub?token=test", nil)
	req.RemoteAddr = remote
	req.Host = host
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	return req
}

func TestPublicOriginDirectRequest(t *testing.T) {
	t.Setenv("FANOUT_PUBLIC_URL", "")
	t.Setenv("FANOUT_TRUSTED_PROXIES", "")
	scheme, host := publicOrigin(publicOriginRequest("203.0.113.5:1234", "panel.internal:8899", nil))
	if scheme != "http" || host != "panel.internal:8899" {
		t.Fatalf("direct origin = %s://%s", scheme, host)
	}
}

func TestPublicOriginTrustedForwardedHeaders(t *testing.T) {
	t.Setenv("FANOUT_PUBLIC_URL", "")
	t.Setenv("FANOUT_TRUSTED_PROXIES", "")
	req := publicOriginRequest("127.0.0.1:1234", "panel.internal:8899", map[string]string{
		"X-Forwarded-Proto": "https",
		"X-Forwarded-Host":  "panel.example.com",
		"X-Forwarded-Port":  "8443",
	})
	scheme, host := publicOrigin(req)
	if scheme != "https" || host != "panel.example.com:8443" {
		t.Fatalf("trusted origin = %s://%s", scheme, host)
	}
}

func TestPublicOriginForwardedRFC7239(t *testing.T) {
	t.Setenv("FANOUT_PUBLIC_URL", "")
	t.Setenv("FANOUT_TRUSTED_PROXIES", "")
	req := publicOriginRequest("10.0.0.2:1234", "panel.internal:8899", map[string]string{
		"Forwarded": `for=192.0.2.1;proto=https;host="panel.example.com"`,
	})
	scheme, host := publicOrigin(req)
	if scheme != "https" || host != "panel.example.com" {
		t.Fatalf("RFC7239 origin = %s://%s", scheme, host)
	}
}

func TestPublicOriginIgnoresUntrustedForwardedHeaders(t *testing.T) {
	t.Setenv("FANOUT_PUBLIC_URL", "")
	t.Setenv("FANOUT_TRUSTED_PROXIES", "")
	req := publicOriginRequest("203.0.113.5:1234", "panel.internal:8899", map[string]string{
		"X-Forwarded-Proto": "https",
		"X-Forwarded-Host":  "attacker.example",
	})
	scheme, host := publicOrigin(req)
	if scheme != "http" || host != "panel.internal:8899" {
		t.Fatalf("spoofed origin accepted: %s://%s", scheme, host)
	}
}

func TestPublicOriginExplicitOverride(t *testing.T) {
	t.Setenv("FANOUT_PUBLIC_URL", "https://panel.example.com")
	t.Setenv("FANOUT_TRUSTED_PROXIES", "")
	scheme, host := publicOrigin(publicOriginRequest("203.0.113.5:1234", "panel.internal:8899", nil))
	if scheme != "https" || host != "panel.example.com" {
		t.Fatalf("explicit origin = %s://%s", scheme, host)
	}
}

func TestPublicOriginTrustedProxyCIDR(t *testing.T) {
	t.Setenv("FANOUT_PUBLIC_URL", "")
	t.Setenv("FANOUT_TRUSTED_PROXIES", "198.51.100.0/24")
	req := publicOriginRequest("198.51.100.7:1234", "panel.internal:8899", map[string]string{
		"X-Forwarded-Proto": "https",
		"X-Forwarded-Host":  "panel.example.com",
	})
	scheme, host := publicOrigin(req)
	if scheme != "https" || host != "panel.example.com" {
		t.Fatalf("trusted CIDR origin = %s://%s", scheme, host)
	}
}

func TestPublicOriginRejectsInvalidForwardedHost(t *testing.T) {
	t.Setenv("FANOUT_PUBLIC_URL", "")
	t.Setenv("FANOUT_TRUSTED_PROXIES", "")
	req := publicOriginRequest("127.0.0.1:1234", "panel.internal:8899", map[string]string{
		"X-Forwarded-Proto": "https",
		"X-Forwarded-Host":  "panel.example.com/path",
	})
	scheme, host := publicOrigin(req)
	if scheme != "http" || host != "panel.internal:8899" {
		t.Fatalf("invalid forwarded host accepted: %s://%s", scheme, host)
	}
}

func TestPublicHostWithoutPortKeepsIPv6Brackets(t *testing.T) {
	t.Setenv("FANOUT_PUBLIC_URL", "https://[2001:db8::1]:8443")
	t.Setenv("FANOUT_TRUSTED_PROXIES", "")
	req := publicOriginRequest("203.0.113.5:1234", "panel.internal:8899", nil)
	if got := publicHostWithoutPort(req); got != "[2001:db8::1]" {
		t.Fatalf("IPv6 host = %q", got)
	}
}

func TestSubFullURLUsesPublicOrigin(t *testing.T) {
	t.Setenv("FANOUT_PUBLIC_URL", "")
	t.Setenv("FANOUT_TRUSTED_PROXIES", "")
	req := publicOriginRequest("127.0.0.1:1234", "panel.internal:8899", map[string]string{
		"X-Forwarded-Proto": "https",
		"X-Forwarded-Host":  "panel.example.com",
	})
	req.TLS = nil
	if got := subFullURL(req, "abc"); got != "https://panel.example.com"+subPath("abc") {
		t.Fatalf("subURL = %q", got)
	}
}
