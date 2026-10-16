package localauth

import (
	"bytes"
	"encoding/json"
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
