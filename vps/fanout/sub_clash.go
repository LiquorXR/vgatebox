package main

// sub_clash.go：主包薄包装，实际纯逻辑下沉至 internal/clash。
// 保留原函数名以兼容现有调用方、契约测试与 Fuzz 语料；
// Windows/macOS 本机可直接 `go test ./internal/clash` 验证同一逻辑。
import (
	"fanout/internal/clash"
	"net/url"
)

// clashUnsupportedError 与 internal/clash.UnsupportedError 为同一类型（别名），
// errors.As 在新旧路径均可识别 mKCP 等不可表达传输。
type clashUnsupportedError = clash.UnsupportedError

func clashProxy(link string) (map[string]any, error) { return clash.Proxy(link) }

func parseClashLink(link string) (*url.URL, int, string, error) { return clash.ParseLink(link) }

func applyClashTransport(p map[string]any, q url.Values) (string, error) {
	return clash.ApplyTransport(p, q)
}

func applyClashSecurity(p map[string]any, q url.Values, transport string) error {
	return clash.ApplySecurity(p, q, transport)
}

func applyClashTLS(p map[string]any, q url.Values, security string) error {
	return clash.ApplyTLS(p, q, security)
}

func clashCertificatePin(q url.Values) (string, bool, error) { return clash.CertificatePin(q) }

func applyClashReality(p map[string]any, q url.Values) error { return clash.ApplyReality(p, q) }

func applyClashVLESSOptions(p map[string]any, q url.Values) error {
	return clash.ApplyVLESSOptions(p, q)
}

func clashWSOpts(q url.Values, upgrade bool) map[string]any { return clash.WSOpts(q, upgrade) }

func clashXHTTPOpts(q url.Values) map[string]any { return clash.XHTTPOpts(q) }

func normalizeCertificatePin(raw string) (string, error) {
	return clash.NormalizeCertificatePin(raw)
}

func renderClash(links []string) ([]byte, []string, error) { return clash.Render(links) }
