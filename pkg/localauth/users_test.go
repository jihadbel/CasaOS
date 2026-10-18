package localauth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func accountRequest(t *testing.T, h http.Handler, method, path string, body interface{}, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(method, path, bytes.NewReader(data))
	r.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func namedLogin(t *testing.T, h http.Handler, name, password string) *http.Cookie {
	t.Helper()
	w := accountRequest(t, h, "POST", "/api/auth/session", map[string]string{"username": name, "password": password}, nil)
	if w.Code != 200 || len(w.Result().Cookies()) != 1 {
		t.Fatal(name, w.Code, w.Body.String())
	}
	return w.Result().Cookies()[0]
}

func createAccount(t *testing.T, h http.Handler, admin *http.Cookie, name string) User {
	t.Helper()
	w := accountRequest(t, h, "POST", "/api/users", map[string]string{"username": name, "password": "test-password-123"}, admin)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var result struct {
		User User `json:"user"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result.User
}

func TestAccountsPersistDistinctIdentitiesAndRevokeOnlyChangedUser(t *testing.T) {
	m, password, h := testManager(t)
	// Only this unit-test fixture uses an unprivileged temporary home. Production
	// uses NewWithStorage, which rejects an unprivileged server identity.
	m.storageRoot = filepath.Join(t.TempDir(), "files")
	m.protectHome = func(path string) error { return os.Chmod(path, 0700) }
	admin := login(t, h, password)
	alice := createAccount(t, h, admin, "alice")
	bob := createAccount(t, h, admin, "bob")
	if alice.ID == bob.ID || alice.Admin || bob.Admin {
		t.Fatal(alice, bob)
	}
	aliceCookie := namedLogin(t, h, "alice", "test-password-123")
	bobCookie := namedLogin(t, h, "bob", "test-password-123")
	for _, entry := range []struct {
		cookie *http.Cookie
		id     string
	}{{aliceCookie, alice.ID}, {bobCookie, bob.ID}} {
		w := accountRequest(t, h, "GET", "/api/auth/session", nil, entry.cookie)
		var response struct {
			Home string `json:"home"`
			User User   `json:"user"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if response.User.ID != entry.id || response.Home != filepath.Join(m.storageRoot, entry.id) {
			t.Fatal(response)
		}
		if denied := accountRequest(t, h, "GET", "/api/users", nil, entry.cookie); denied.Code != 403 {
			t.Fatal("non-admin listed accounts", denied.Code)
		}
		if denied := accountRequest(t, h, "POST", "/api/users", map[string]string{"username": "intruder", "password": "test-password-123"}, entry.cookie); denied.Code != 403 {
			t.Fatal("non-admin created account", denied.Code)
		}
	}
	reset := accountRequest(t, h, "PATCH", "/api/users/"+alice.ID, map[string]string{"password": "replacement-password-456"}, admin)
	if reset.Code != 200 {
		t.Fatal(reset.Code, reset.Body.String())
	}
	if w := accountRequest(t, h, "GET", "/api/trash", nil, aliceCookie); w.Code != 401 {
		t.Fatal("reset user session retained", w.Code)
	}
	if w := accountRequest(t, h, "GET", "/api/trash", nil, bobCookie); w.Code != 204 {
		t.Fatal("other user session revoked", w.Code)
	}
	if w := accountRequest(t, h, "GET", "/api/users", nil, admin); w.Code != 200 || strings.Contains(w.Body.String(), "passwordHash") {
		t.Fatal("password verifier exposed", w.Body.String())
	}
	if w := accountRequest(t, h, "POST", "/api/users", map[string]string{"username": "ALICE", "password": "test-password-123"}, admin); w.Code != 409 {
		t.Fatal("duplicate account allowed", w.Code)
	}
	reloaded, err := New(m.dir)
	if err != nil {
		t.Fatal(err)
	}
	reloadedHandler := reloaded.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	namedLogin(t, reloadedHandler, "alice", "replacement-password-456")
	namedLogin(t, reloadedHandler, "bob", "test-password-123")
	listing := accountRequest(t, h, "PATCH", "/api/users/"+bob.ID, map[string]bool{"disabled": true}, admin)
	if listing.Code != 200 {
		t.Fatal(listing.Code, listing.Body.String())
	}
	if w := accountRequest(t, h, "GET", "/api/trash", nil, bobCookie); w.Code != 401 {
		t.Fatal("disabled user's cookie accepted", w.Code)
	}
	if w := accountRequest(t, h, "POST", "/api/auth/session", map[string]string{"username": "bob", "password": "test-password-123"}, nil); w.Code != 401 {
		t.Fatal("disabled user logged in", w.Code)
	}
	if _, err := os.Stat(filepath.Join(m.storageRoot, bob.ID)); err != nil {
		t.Fatal("disabling account removed its files", err)
	}
	if w := accountRequest(t, h, "DELETE", "/api/users/admin", nil, admin); w.Code != 400 {
		t.Fatal("administrator could be disabled", w.Code)
	}
}

func TestUserCanChangeOwnPasswordWithoutChangingAdministrator(t *testing.T) {
	_, password, h := testManager(t)
	admin := login(t, h, password)
	user := createAccount(t, h, admin, "alice")
	_ = user
	cookie := namedLogin(t, h, "alice", "test-password-123")
	w := accountRequest(t, h, "POST", "/api/auth/password", map[string]string{"password": "new-user-password-456"}, cookie)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	namedLogin(t, h, "alice", "new-user-password-456")
	login(t, h, password)
	if w := accountRequest(t, h, "GET", "/api/users", nil, admin); w.Code != 200 {
		t.Fatal("user rotation revoked administrator", w.Code)
	}
}
