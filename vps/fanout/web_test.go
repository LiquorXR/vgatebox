package main

import (
	"compress/gzip"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHandleIndexRedirectsNextWithoutSlash(t *testing.T) {
	rec := httptest.NewRecorder()
	handleIndex(rec, httptest.NewRequest(http.MethodGet, "/next?x=1", nil))
	if rec.Code != http.StatusTemporaryRedirect {
		t.Fatalf("status = %d", rec.Code)
	}
	if got, want := rec.Header().Get("Location"), currentBasePath()+"/next/?x=1"; got != want {
		t.Fatalf("location = %q, want %q", got, want)
	}
}

func TestLegacyAdvancedRedirectsToAccessTab(t *testing.T) {
	rec := httptest.NewRecorder()
	handleIndex(rec, httptest.NewRequest(http.MethodGet, "/advanced", nil))
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d", rec.Code)
	}
	if got, want := rec.Header().Get("Location"), currentBasePath()+"/#/access"; got != want {
		t.Fatalf("location = %q, want %q", got, want)
	}
}

func TestRootServesNewConsole(t *testing.T) {
	if !webUIAvailable() {
		t.Skip("frontend not built")
	}
	rec := httptest.NewRecorder()
	handleIndex(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("cache-control = %q", got)
	}
	if !strings.Contains(rec.Header().Get("Content-Security-Policy"), "script-src 'self'") {
		t.Fatal("missing strict script CSP")
	}
	if !strings.Contains(rec.Body.String(), `id="app"`) {
		t.Fatal("new console body missing")
	}
}

func TestServeLoginPageIsNoStore(t *testing.T) {
	if !webUIAvailable() {
		t.Skip("frontend not built")
	}
	rec := httptest.NewRecorder()
	serveLoginPage(rec)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("cache-control = %q", got)
	}
	if !strings.Contains(rec.Header().Get("Content-Security-Policy"), "script-src 'self'") {
		t.Fatal("login page missing strict script CSP")
	}
}

func TestGzipMiddlewareOnlyCompressesText(t *testing.T) {
	handler := withGzip(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Header().Get("Content-Encoding") != "gzip" {
		t.Fatal("expected gzip response")
	}
	reader, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(reader)
	_ = reader.Close()
	if string(body) != `{"ok":true}` {
		t.Fatalf("body = %q", body)
	}

	image := withGzip(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte{1, 2, 3})
	}))
	rec = httptest.NewRecorder()
	image.ServeHTTP(rec, req)
	if rec.Header().Get("Content-Encoding") != "" {
		t.Fatal("image response must not be gzipped")
	}
}

func TestBuiltConsoleServesWithCSP(t *testing.T) {
	if !webUIAvailable() {
		t.Skip("frontend not built")
	}
	rec := httptest.NewRecorder()
	serveWebFile(rec, httptest.NewRequest(http.MethodGet, "/next/", nil), "index.html")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("index must not be cached")
	}
	if !strings.Contains(rec.Header().Get("Content-Security-Policy"), "script-src 'self'") {
		t.Fatal("missing strict script CSP")
	}
}

func TestHashedAssetsAreImmutable(t *testing.T) {
	entries, err := fs.ReadDir(webDistFS, "assets")
	if err != nil {
		t.Skip("frontend not built")
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".js") {
			rec := httptest.NewRecorder()
			serveWebFile(rec, httptest.NewRequest(http.MethodGet, "/next/assets/"+entry.Name(), nil), "assets/"+entry.Name())
			if rec.Code != http.StatusOK {
				t.Fatalf("asset status = %d", rec.Code)
			}
			if rec.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
				t.Fatalf("asset cache-control = %q", rec.Header().Get("Cache-Control"))
			}
			return
		}
	}
	t.Fatal("no hashed JavaScript asset found")
}

func TestPublicAssetsBypassLogin(t *testing.T) {
	for _, path := range []string{"/assets/main.js", "/next/assets/main.js", "/favicon.svg", "/next/favicon.svg"} {
		if !isPublicAsset(path) {
			t.Fatalf("%s should be public", path)
		}
	}
	for _, path := range []string{"/", "/next/", "/api/console", "/advanced"} {
		if isPublicAsset(path) {
			t.Fatalf("%s must stay authenticated", path)
		}
	}
}

func TestWrapServesLoginAssetsWithoutSession(t *testing.T) {
	auth := &Auth{sessions: map[string]time.Time{}, fails: map[string]*loginFails{}}
	called := false
	handler := auth.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/main.js", nil))
	if !called || rec.Code != http.StatusOK {
		t.Fatalf("asset was not passed through: called=%v status=%d", called, rec.Code)
	}
}

func TestSecurityHeaders(t *testing.T) {
	rec := httptest.NewRecorder()
	withSecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})).ServeHTTP(
		rec, httptest.NewRequest(http.MethodGet, "/", nil),
	)
	if rec.Header().Get("X-Frame-Options") != "DENY" {
		t.Fatal("missing frame protection")
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("missing nosniff")
	}
}
