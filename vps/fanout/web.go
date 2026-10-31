package main

import (
	"embed"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
)

// webdist is the Vite build output. A fresh checkout only contains .gitkeep, so
// the Go package still compiles before the frontend has been built.
//
//go:embed all:webdist
var webDist embed.FS

var webDistFS fs.FS

func init() {
	sub, err := fs.Sub(webDist, "webdist")
	if err != nil {
		panic(err)
	}
	webDistFS = sub
}

func webAsset(name string) ([]byte, bool) {
	data, err := fs.ReadFile(webDistFS, name)
	if err != nil {
		return nil, false
	}
	return data, true
}

func webUIAvailable() bool {
	_, err := fs.Stat(webDistFS, "index.html")
	return err == nil
}

func isPublicAsset(path string) bool {
	return strings.HasPrefix(path, "/assets/") ||
		strings.HasPrefix(path, "/next/assets/") ||
		path == "/favicon.svg" ||
		path == "/next/favicon.svg"
}

const newUICSP = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; font-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'"

var webContentTypes = map[string]string{
	".html":  "text/html; charset=utf-8",
	".js":    "text/javascript; charset=utf-8",
	".css":   "text/css; charset=utf-8",
	".json":  "application/json; charset=utf-8",
	".map":   "application/json; charset=utf-8",
	".svg":   "image/svg+xml",
	".png":   "image/png",
	".woff2": "font/woff2",
	".ico":   "image/x-icon",
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/":
		serveWebFile(w, r, "index.html")
	case r.URL.Path == "/advanced":
		http.Redirect(w, r, currentBasePath()+"/#/access", http.StatusFound)
	case r.URL.Path == "/next":
		target := currentBasePath() + "/next/"
		if r.URL.RawQuery != "" {
			target += "?" + r.URL.RawQuery
		}
		http.Redirect(w, r, target, http.StatusTemporaryRedirect)
	case strings.HasPrefix(r.URL.Path, "/next/"):
		serveWebFile(w, r, strings.TrimPrefix(r.URL.Path, "/next/"))
	case strings.HasPrefix(r.URL.Path, "/assets/"):
		// The login page is served at /login, so its relative asset URLs resolve
		// to /<base>/assets/. Serve the same hashed assets from both prefixes.
		serveWebFile(w, r, strings.TrimPrefix(r.URL.Path, "/"))
	case r.URL.Path == "/favicon.svg":
		serveWebFile(w, r, strings.TrimPrefix(r.URL.Path, "/"))
	default:
		http.NotFound(w, r)
	}
}

func serveWebFile(w http.ResponseWriter, r *http.Request, name string) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if name == "" || strings.HasSuffix(name, "/") {
		name += "index.html"
	}
	if strings.Contains(name, "..") || strings.HasPrefix(name, "/") {
		http.NotFound(w, r)
		return
	}

	data, ok := webAsset(name)
	if !ok {
		if name == "index.html" {
			serveFrontendNotBuilt(w)
			return
		}
		http.NotFound(w, r)
		return
	}

	ext := path.Ext(name)
	contentType := webContentTypes[ext]
	if contentType == "" {
		contentType = mime.TypeByExtension(ext)
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")

	if strings.HasSuffix(name, ".html") {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", newUICSP)
	} else if strings.HasPrefix(name, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		// theme-init.js and favicon.svg are stable names, not content-addressed.
		w.Header().Set("Cache-Control", "no-cache")
	}

	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(data)
}

func serveLoginPage(w http.ResponseWriter) {
	if data, ok := webAsset("login.html"); ok {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", newUICSP)
		_, _ = w.Write(data)
		return
	}
	serveFrontendNotBuilt(w)
}

func serveFrontendNotBuilt(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = w.Write([]byte(`<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>vgatebox · 前端未构建</title>
<style>body{margin:0;background:#f4f6f7;color:#0e1b20;font:14px/1.6 -apple-system,BlinkMacSystemFont,"PingFang SC","Microsoft YaHei",sans-serif;display:grid;place-items:center;min-height:100dvh;padding:24px}main{max-width:560px;background:#fff;border:1px solid #e2e8eb;border-radius:14px;padding:28px}h1{font-size:20px;margin:0 0 12px}p{margin:0 0 10px;color:#5d7078}code{background:#f0f4f4;border-radius:6px;padding:2px 6px;font-family:ui-monospace,Consolas,monospace}</style>
</head><body><main><h1>新的控制台尚未构建</h1><p>这个二进制里没有前端产物。请在源码目录执行：</p><p><code>cd vps/fanout/frontend &amp;&amp; npm ci &amp;&amp; npm run build</code></p><p>然后重新构建 Go 程序。</p></main></body></html>`))
}
