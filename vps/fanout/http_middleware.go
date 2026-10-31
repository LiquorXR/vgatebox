package main

import (
	"compress/gzip"
	"net/http"
	"strings"
	"sync"
)

var gzipPool = sync.Pool{New: func() any { return gzip.NewWriter(nil) }}

type gzipResponseWriter struct {
	http.ResponseWriter
	gz          *gzip.Writer
	wroteHeader bool
	compress    bool
	status      int
}

func (w *gzipResponseWriter) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	w.status = status

	contentType := w.Header().Get("Content-Type")
	w.compress = status != http.StatusNoContent &&
		status != http.StatusNotModified &&
		w.Header().Get("Content-Encoding") == "" &&
		!strings.HasPrefix(contentType, "image/") &&
		!strings.HasPrefix(contentType, "font/")
	if w.compress {
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Del("Content-Length")
		w.Header().Set("Vary", "Accept-Encoding")
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *gzipResponseWriter) Write(data []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	if w.compress {
		return w.gz.Write(data)
	}
	return w.ResponseWriter.Write(data)
}

func (w *gzipResponseWriter) Flush() {
	if w.compress {
		_ = w.gz.Flush()
	}
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// withGzip compresses textual responses when the client advertises gzip. Images,
// fonts and empty responses pass through unchanged.
func withGzip(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Vary", "Accept-Encoding")
		if r.Method == http.MethodHead || !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}
		gz := gzipPool.Get().(*gzip.Writer)
		gz.Reset(w)
		gw := &gzipResponseWriter{ResponseWriter: w, gz: gz}
		defer func() {
			if gw.compress {
				_ = gz.Close()
			}
			gzipPool.Put(gz)
		}()
		next.ServeHTTP(gw, r)
	})
}

// withSecurityHeaders applies headers that are safe for both the legacy and new
// consoles. A strict CSP is set only on the new UI pages, because the legacy
// console relies on inline scripts and styles.
func withSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		next.ServeHTTP(w, r)
	})
}
