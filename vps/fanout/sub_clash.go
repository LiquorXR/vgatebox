package main

// sub_clash.go：主包薄包装，实际纯逻辑下沉至 internal/clash。
// 保留调用方实际使用的 clashProxy/renderClash；
// Windows/macOS 本机可直接 `go test ./internal/clash` 验证同一逻辑。
import (
	"fanout/internal/clash"
)

func clashProxy(link string) (map[string]any, error) { return clash.Proxy(link) }

func renderClash(links []string) ([]byte, []string, error) { return clash.Render(links) }
