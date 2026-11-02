package main

import (
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// publicOrigin returns the scheme and host clients should use to reach this
// instance. A reverse proxy normally terminates TLS and forwards plain HTTP,
// so request-local r.TLS/r.Host alone would generate http://internal-host links.
//
// FANOUT_PUBLIC_URL is the explicit override. Otherwise forwarded headers are
// accepted only from a loopback/private peer or a configured trusted proxy;
// arbitrary public clients must not be able to rewrite links with headers.
func publicOrigin(r *http.Request) (string, string) {
	if raw := strings.TrimSpace(os.Getenv("FANOUT_PUBLIC_URL")); raw != "" {
		if u, err := url.Parse(raw); err == nil && (strings.EqualFold(u.Scheme, "http") || strings.EqualFold(u.Scheme, "https")) && u.User == nil && u.Host != "" {
			return strings.ToLower(u.Scheme), u.Host
		}
	}
	if trustedProxyRequest(r) {
		if scheme, host, ok := forwardedOrigin(r); ok {
			return scheme, host
		}
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	host := strings.TrimSpace(r.Host)
	if validPublicHost(host) {
		return scheme, host
	}
	if ip := hostPublicIP(); ip != "" {
		return scheme, net.JoinHostPort(ip, strconv.Itoa(getWebSettings().Port))
	}
	return scheme, ""
}

// publicHostWithoutPort returns the host used in share links, which carry the
// inbound port separately. The caller has already checked hostPublicIP.
func publicHostWithoutPort(r *http.Request) string {
	_, host := publicOrigin(r)
	if h, _, err := net.SplitHostPort(host); err == nil {
		if strings.Contains(h, ":") {
			return "[" + h + "]"
		}
		return h
	}
	return strings.TrimSpace(host)
}

func trustedProxyRequest(r *http.Request) bool {
	remote := strings.TrimSpace(r.RemoteAddr)
	if remote == "" {
		return false
	}
	host := remote
	if h, _, err := net.SplitHostPort(remote); err == nil {
		host = h
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	for _, entry := range strings.Split(os.Getenv("FANOUT_TRUSTED_PROXIES"), ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if _, network, err := net.ParseCIDR(entry); err == nil && network.Contains(ip) {
			return true
		}
		if trusted := net.ParseIP(entry); trusted != nil && trusted.Equal(ip) {
			return true
		}
	}
	return false
}

func forwardedOrigin(r *http.Request) (string, string, bool) {
	scheme := ""
	host := ""
	port := ""

	if raw := firstHeaderValue(r.Header.Get("Forwarded")); raw != "" {
		for _, part := range strings.Split(raw, ";") {
			key, value, ok := strings.Cut(part, "=")
			if !ok {
				continue
			}
			switch strings.ToLower(strings.TrimSpace(key)) {
			case "proto":
				scheme = unquoteHeaderValue(value)
			case "host":
				host = unquoteHeaderValue(value)
			}
		}
	}
	if scheme == "" {
		scheme = firstHeaderValue(r.Header.Get("X-Forwarded-Proto"))
	}
	if host == "" {
		host = firstHeaderValue(r.Header.Get("X-Forwarded-Host"))
	}
	if port == "" {
		port = firstHeaderValue(r.Header.Get("X-Forwarded-Port"))
	}
	if scheme == "" {
		scheme = "http"
		if r.TLS != nil {
			scheme = "https"
		}
	}
	if host == "" {
		host = strings.TrimSpace(r.Host)
	}
	if port != "" && !hostHasPort(host) && validPort(port) {
		host = hostWithPort(host, port)
	}
	scheme = strings.ToLower(scheme)
	if (scheme != "http" && scheme != "https") || !validPublicHost(host) {
		return "", "", false
	}
	return scheme, host, true
}

func firstHeaderValue(value string) string {
	if first, _, ok := strings.Cut(value, ","); ok {
		value = first
	}
	return unquoteHeaderValue(value)
}

func unquoteHeaderValue(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		if unquoted, err := strconv.Unquote(value); err == nil {
			return strings.TrimSpace(unquoted)
		}
	}
	return value
}

func hostHasPort(host string) bool {
	_, _, err := net.SplitHostPort(host)
	return err == nil
}

func validPort(port string) bool {
	n, err := strconv.Atoi(port)
	return err == nil && n > 0 && n <= 65535
}

func hostWithPort(host, port string) string {
	host = strings.TrimSpace(host)
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		return host + ":" + port
	}
	return net.JoinHostPort(host, port)
}

func validPublicHost(host string) bool {
	host = strings.TrimSpace(host)
	if host == "" || strings.ContainsAny(host, "/\\?#@ \t\r\n") {
		return false
	}
	u, err := url.Parse("//" + host)
	if err != nil || u.User != nil || u.Host != host || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	if u.Hostname() == "" {
		return false
	}
	if port := u.Port(); port != "" && !validPort(port) {
		return false
	}
	return true
}
