package localauth

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type blockedBody struct {
	started, release chan struct{}
	reader           io.Reader
}

func (b *blockedBody) Read(p []byte) (int, error) {
	select {
	case <-b.started:
	default:
		close(b.started)
	}
	<-b.release
	return b.reader.Read(p)
}

func TestPasswordRotationRejectsInFlightOldPasswordLogin(t *testing.T) {
	_, password, h := testManager(t)
	cookie := login(t, h, password)
	body := &blockedBody{make(chan struct{}), make(chan struct{}), strings.NewReader(`{"password":"` + password + `"}`)}
	w := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.ServeHTTP(w, httptest.NewRequest("POST", "http://casa.test/api/auth/session", body))
	}()
	<-body.started
	change := httptest.NewRequest("POST", "http://casa.test/api/auth/password", strings.NewReader(`{"password":"replacement-password-123"}`))
	change.AddCookie(cookie)
	rotated := httptest.NewRecorder()
	h.ServeHTTP(rotated, change)
	close(body.release)
	<-done
	if rotated.Code != 200 || w.Code != 401 || len(w.Result().Cookies()) != 0 {
		t.Fatal("old password created a session after rotation", rotated.Code, w.Code, w.Result().Cookies())
	}
	login(t, h, "replacement-password-123")
}

func TestPasswordChangeRechecksRevokedSession(t *testing.T) {
	_, password, h := testManager(t)
	cookie := login(t, h, password)
	body := &blockedBody{make(chan struct{}), make(chan struct{}), strings.NewReader(`{"password":"replacement-password-123"}`)}
	req := httptest.NewRequest("POST", "http://casa.test/api/auth/password", body)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { defer close(done); h.ServeHTTP(w, req) }()
	<-body.started
	logout := httptest.NewRequest("DELETE", "http://casa.test/api/auth/session", nil)
	logout.AddCookie(cookie)
	h.ServeHTTP(httptest.NewRecorder(), logout)
	close(body.release)
	<-done
	if w.Code != 401 {
		t.Fatal("revoked session changed the password", w.Code)
	}
	login(t, h, password)
}

func testManager(t *testing.T) (*Manager, string, http.Handler) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "auth")
	m, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	password, err := os.ReadFile(filepath.Join(dir, "initial-password.txt"))
	if err != nil {
		t.Fatal(err)
	}
	handler := m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" && !Authenticated(r) {
			t.Error("missing authentication context")
		}
		w.WriteHeader(204)
	}))
	return m, strings.TrimSpace(string(password)), handler
}

func login(t *testing.T, h http.Handler, password string) *http.Cookie {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"password": password})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "http://casa.test/api/auth/session", bytes.NewReader(body)))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal(cookies)
	}
	c := cookies[0]
	if !c.HttpOnly || c.SameSite != http.SameSiteStrictMode {
		t.Fatal("unsafe session cookie", c)
	}
	return c
}

func TestAuthenticationProtectsEveryAPIFamily(t *testing.T) {
	_, password, h := testManager(t)
	for _, path := range []string{"/v1/file", "/v2/casaos", "/v3/file", "/doc", "/api/zip", "/api/unzip", "/api/trash", "/api/media/timeline", "/api/settings", "/api/server-info"} {
		req := httptest.NewRequest("GET", "http://casa.test"+path, nil)
		req.RemoteAddr = "192.0.2.1:12345"
		req.Header.Set("X-Forwarded-For", "127.0.0.1")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != 401 {
			t.Fatal(path, w.Code)
		}
	}
	cookie := login(t, h, password)
	req := httptest.NewRequest("GET", "http://casa.test/api/trash", nil)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 204 {
		t.Fatal(w.Code)
	}
}

func TestAuthenticationRejectsCrossOriginAndExpiredSessions(t *testing.T) {
	m, password, h := testManager(t)
	cookie := login(t, h, password)
	req := httptest.NewRequest("POST", "http://casa.test/api/trash/empty", nil)
	req.AddCookie(cookie)
	req.Header.Set("Origin", "https://other.test")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
	m.mu.Lock()
	m.sessions[cookie.Value] = time.Now().Add(-time.Second)
	m.mu.Unlock()
	req = httptest.NewRequest("GET", "http://casa.test/api/trash", nil)
	req.AddCookie(cookie)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 401 {
		t.Fatal(w.Code)
	}
}

func TestSharedFileNavigationRedirectsToLoginWithoutExposingContents(t *testing.T) {
	_, _, h := testManager(t)
	req := httptest.NewRequest("GET", "http://casa.test/v1/file?path=C%3A%2Fprivate.txt", nil)
	req.Header.Set("Accept", "text/html")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther || !strings.HasPrefix(w.Header().Get("Location"), "/?next=") {
		t.Fatal(w.Code, w.Header())
	}
}

func TestPasswordChangeInvalidatesSessionsAndPersists(t *testing.T) {
	m, password, h := testManager(t)
	cookie := login(t, h, password)
	newPassword := "a-new-password-for-testing"
	body, _ := json.Marshal(map[string]string{"password": newPassword})
	req := httptest.NewRequest("POST", "http://casa.test/api/auth/password", bytes.NewReader(body))
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	req = httptest.NewRequest("GET", "http://casa.test/api/trash", nil)
	req.AddCookie(cookie)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 401 {
		t.Fatal("old session still accepted")
	}
	if _, err := os.Stat(filepath.Join(m.dir, "initial-password.txt")); !os.IsNotExist(err) {
		t.Fatal("bootstrap password retained", err)
	}
	reloaded, err := New(m.dir)
	if err != nil {
		t.Fatal(err)
	}
	login(t, reloaded.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})), newPassword)
}

func TestFullAttemptTableEvictsOldestWithoutGlobalLockout(t *testing.T) {
	m, password, handler := testManager(t)
	for _, entry := range []struct {
		host           string
		known, blocked bool
		want           int
	}{
		{"127.0.0.1", false, false, 200},
		{"198.51.100.1", false, false, 200},
		{"198.51.100.2", true, false, 200},
		{"198.51.100.3", true, true, 429},
	} {
		t.Run(entry.host, func(t *testing.T) {
			m.mu.Lock()
			m.attempts = make(map[string]attempt)
			now := time.Now()
			for i := 0; i < maxAttemptHosts; i++ {
				m.attempts[fmt.Sprintf("host-%04d", i)] = attempt{count: 1, until: now.Add(time.Minute), last: now.Add(time.Duration(i) * time.Millisecond)}
			}
			if entry.known {
				delete(m.attempts, "host-1023")
				count := 1
				if entry.blocked {
					count = maxHostAttempts
				}
				m.attempts[entry.host] = attempt{count: count, until: now.Add(time.Minute), last: now}
			}
			m.mu.Unlock()
			body, err := json.Marshal(map[string]string{"password": password})
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest("POST", "/api/auth/session", bytes.NewReader(body))
			req.RemoteAddr = entry.host + ":1234"
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			if w.Code != entry.want {
				t.Fatal(w.Code, w.Body.String())
			}
			m.mu.Lock()
			_, oldestPresent := m.attempts["host-0000"]
			count := len(m.attempts)
			m.mu.Unlock()
			if count > maxAttemptHosts || !entry.known && oldestPresent {
				t.Fatal("eviction did not bound table or remove oldest host", count, oldestPresent)
			}
		})
	}
}

func TestSameOriginTrustsForwardedSchemeOnlyFromDirectLoopback(t *testing.T) {
	for _, entry := range []struct {
		name, remote, origin string
		proto                []string
		tls, crossSite, want bool
	}{
		{"direct-http", "192.0.2.1:1234", "http://casa.test", nil, false, false, true},
		{"direct-tls", "192.0.2.1:1234", "https://casa.test", nil, true, false, true},
		{"local-proxy-v4", "127.0.0.1:1234", "https://casa.test", []string{"https"}, false, false, true},
		{"local-proxy-v6", "[::1]:1234", "https://casa.test", []string{"https"}, false, false, true},
		{"remote-spoof", "192.0.2.1:1234", "https://casa.test", []string{"https"}, false, false, false},
		{"malformed-remote", "localhost", "https://casa.test", []string{"https"}, false, false, false},
		{"chained-proto", "127.0.0.1:1234", "https://casa.test", []string{"https,http"}, false, false, false},
		{"duplicate-proto", "127.0.0.1:1234", "https://casa.test", []string{"https", "http"}, false, false, false},
		{"different-host", "127.0.0.1:1234", "https://other.test", []string{"https"}, false, false, false},
		{"different-port", "127.0.0.1:1234", "https://casa.test:1234", []string{"https"}, false, false, false},
		{"cross-site", "127.0.0.1:1234", "https://casa.test", []string{"https"}, false, true, false},
		{"origin-with-path", "127.0.0.1:1234", "https://casa.test/path", []string{"https"}, false, false, false},
	} {
		t.Run(entry.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "http://casa.test/api/auth/session", nil)
			req.RemoteAddr = entry.remote
			req.Header.Set("Origin", entry.origin)
			req.Header.Set("X-Forwarded-For", "127.0.0.1")
			for _, proto := range entry.proto {
				req.Header.Add("X-Forwarded-Proto", proto)
			}
			if entry.tls {
				req.TLS = &tls.ConnectionState{}
			}
			if entry.crossSite {
				req.Header.Set("Sec-Fetch-Site", "cross-site")
			}
			if got := sameOrigin(req); got != entry.want {
				t.Fatal(got, entry.want)
			}
		})
	}
}

func TestLocalHTTPSProxyLoginAndLogoutSetSecureCookies(t *testing.T) {
	_, password, handler := testManager(t)
	body, err := json.Marshal(map[string]string{"password": password})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "http://casa.test/api/auth/session", bytes.NewReader(body))
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("Origin", "https://casa.test")
	req.Header.Set("X-Forwarded-Proto", "https")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != 200 || len(w.Result().Cookies()) != 1 || !w.Result().Cookies()[0].Secure {
		t.Fatal(w.Code, w.Header(), w.Body.String())
	}
	cookie := w.Result().Cookies()[0]
	protected := httptest.NewRequest("POST", "http://casa.test/api/settings", nil)
	protected.RemoteAddr = req.RemoteAddr
	protected.Header = req.Header.Clone()
	protected.AddCookie(cookie)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, protected)
	if w.Code != 204 {
		t.Fatal("proxy session rejected", w.Code)
	}
	logout := httptest.NewRequest("DELETE", "http://casa.test/api/auth/session", nil)
	logout.RemoteAddr = req.RemoteAddr
	logout.Header = req.Header.Clone()
	logout.AddCookie(cookie)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, logout)
	if w.Code != 200 || len(w.Result().Cookies()) != 1 || !w.Result().Cookies()[0].Secure || w.Result().Cookies()[0].MaxAge != -1 {
		t.Fatal(w.Code, w.Header())
	}
}
