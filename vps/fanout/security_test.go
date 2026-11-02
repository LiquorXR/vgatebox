package main

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestPostOnlyRejectsNonPost(t *testing.T) {
	called := false
	h := postOnly(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	})
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		rec := httptest.NewRecorder()
		h(rec, httptest.NewRequest(method, "/api/x", nil))
		if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != http.MethodPost {
			t.Fatalf("%s: code=%d allow=%q", method, rec.Code, rec.Header().Get("Allow"))
		}
		if called {
			t.Fatalf("%s reached the handler", method)
		}
	}
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodPost, "/api/x", nil))
	if !called || rec.Code != http.StatusNoContent {
		t.Fatalf("POST not passed through: called=%v code=%d", called, rec.Code)
	}
}

// Guarded routes must reject GET before touching their manager state.
func TestPostOnlyRouteRejectsGet(t *testing.T) {
	m := NewManager(1, t.TempDir())
	rec := httptest.NewRecorder()
	postOnly(apiStop(m))(rec, httptest.NewRequest(http.MethodGet, "/api/stop?slot=1", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET /api/stop = %d, want 405", rec.Code)
	}

	rec = httptest.NewRecorder()
	apiCred(m)(rec, httptest.NewRequest(http.MethodPost, "/api/cred", strings.NewReader("{}")))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty cred body = %d, want 400", rec.Code)
	}
}

func TestWrapRejectsCrossSiteWrites(t *testing.T) {
	a := &Auth{sessions: map[string]time.Time{}, fails: map[string]*loginFails{}}
	a.sessions["tok"] = time.Now().Add(time.Hour)
	called := 0
	h := a.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
		w.WriteHeader(http.StatusNoContent)
	}))
	req := func(method, site string) *http.Request {
		r := httptest.NewRequest(method, "/api/stop?slot=1", nil)
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: "tok"})
		if site != "" {
			r.Header.Set("Sec-Fetch-Site", site)
		}
		return r
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req(http.MethodPost, "cross-site"))
	if rec.Code != http.StatusForbidden || called != 0 {
		t.Fatalf("cross-site POST: code=%d called=%d", rec.Code, called)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req(http.MethodPost, "same-origin"))
	if rec.Code != http.StatusNoContent || called != 1 {
		t.Fatalf("same-origin POST: code=%d called=%d", rec.Code, called)
	}

	// Cross-site GET navigation stays allowed; it is read-only and POST-only
	// guards protect every mutation.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req(http.MethodGet, "cross-site"))
	if rec.Code != http.StatusNoContent || called != 2 {
		t.Fatalf("cross-site GET: code=%d called=%d", rec.Code, called)
	}
}

func TestWriteJSONNoStore(t *testing.T) {
	rec := httptest.NewRecorder()
	writeJSON(rec, http.StatusOK, map[string]string{"ok": "1"})
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
}

func TestRequestIsHTTPS(t *testing.T) {
	t.Setenv("FANOUT_PUBLIC_URL", "https://panel.example.com")

	r := httptest.NewRequest(http.MethodGet, "http://10.0.0.1/", nil)
	r.RemoteAddr = "203.0.113.5:1234"
	r.Header.Set("X-Forwarded-Proto", "https")
	if requestIsHTTPS(r) {
		t.Fatal("untrusted proxy header must be ignored")
	}

	r = httptest.NewRequest(http.MethodGet, "http://10.0.0.1/", nil)
	r.RemoteAddr = "127.0.0.1:1234"
	r.Header.Set("X-Forwarded-Proto", "https")
	if !requestIsHTTPS(r) {
		t.Fatal("trusted proxy https must be detected")
	}

	r = httptest.NewRequest(http.MethodGet, "https://panel.example.com/", nil)
	r.TLS = &tls.ConnectionState{}
	if !requestIsHTTPS(r) {
		t.Fatal("direct TLS must be detected")
	}
}

func TestLoginCookieSecureOnlyOnHTTPS(t *testing.T) {
	a := newTestAuth()
	form := url.Values{"password": {"secret"}}.Encode()
	for _, tc := range []struct {
		name   string
		secure bool
	}{
		{"https", true},
		{"http", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "http://panel.example.com/login", strings.NewReader(form))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			if tc.secure {
				r.TLS = &tls.ConnectionState{}
			}
			rec := httptest.NewRecorder()
			a.handleLogin(rec, r)
			if rec.Code != http.StatusOK {
				t.Fatalf("login code = %d", rec.Code)
			}
			cookies := rec.Result().Cookies()
			if len(cookies) != 1 || cookies[0].Name != sessionCookie {
				t.Fatalf("unexpected cookies: %v", cookies)
			}
			if cookies[0].Secure != tc.secure {
				t.Fatalf("Secure = %v, want %v", cookies[0].Secure, tc.secure)
			}
		})
	}
}

func TestHSTSOnlyOnHTTPS(t *testing.T) {
	for _, tc := range []struct {
		name   string
		secure bool
		want   string
	}{
		{"https", true, "max-age=31536000"},
		{"http", false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "http://panel.example.com/", nil)
			if tc.secure {
				r.TLS = &tls.ConnectionState{}
			}
			rec := httptest.NewRecorder()
			withSecurityHeaders(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(rec, r)
			if got := rec.Header().Get("Strict-Transport-Security"); got != tc.want {
				t.Fatalf("Strict-Transport-Security = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestHTTPServerHasResourceLimits(t *testing.T) {
	srv := newHTTPServer(http.NotFoundHandler())
	if srv.ReadHeaderTimeout != 5*time.Second ||
		srv.ReadTimeout != 60*time.Second ||
		srv.WriteTimeout != 120*time.Second ||
		srv.IdleTimeout != 120*time.Second ||
		srv.MaxHeaderBytes != 1<<20 {
		t.Fatal("management server timeouts or header limit changed unexpectedly")
	}
}

func TestSettingsPasswordRequiresCurrentAndInvalidatesOtherSessions(t *testing.T) {
	auth, srv, _ := managementFixture(t)
	keep, err := auth.issue()
	if err != nil {
		t.Fatal(err)
	}
	other, err := auth.issue()
	if err != nil {
		t.Fatal(err)
	}
	handler := apiSettings(auth, srv)
	post := func(body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/api/settings", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: keep})
		rec := httptest.NewRecorder()
		handler(rec, r)
		return rec
	}

	if rec := post(`{"password":"fixture-new-password"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("missing current password = %d, want 400", rec.Code)
	}
	if rec := post(`{"password":"fixture-new-password","current_password":"wrong"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("wrong current password = %d, want 400", rec.Code)
	}
	if !auth.check("fixture-old-password") {
		t.Fatal("password changed without verification")
	}

	if rec := post(`{"password":"fixture-new-password","current_password":"fixture-old-password"}`); rec.Code != http.StatusOK {
		t.Fatalf("valid password change = %d, want 200", rec.Code)
	}
	if !auth.check("fixture-new-password") {
		t.Fatal("new password not applied")
	}
	if !auth.valid(keep) {
		t.Fatal("current session should stay valid")
	}
	if auth.valid(other) {
		t.Fatal("other sessions should be invalidated")
	}
}
