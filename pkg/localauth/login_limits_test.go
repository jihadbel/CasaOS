package localauth

import (
	"net/http"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func fastAccountHash(t *testing.T, m *Manager, id, password string) {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	record := m.users[id]
	record.Hash = string(hash)
	m.users[id] = record
	m.mu.Unlock()
}

func TestMemberSessionsCannotBlockOtherAccounts(t *testing.T) {
	m, password, h := testManager(t)
	admin := login(t, h, password)
	alice := createAccount(t, h, admin, "alice")
	createAccount(t, h, admin, "bob")
	bobCookie := namedLogin(t, h, "bob", "test-password-123")
	fastAccountHash(t, m, alice.ID, "test-password-123")
	var first, latest *http.Cookie
	for i := 0; i < maxSessionsPerUser+3; i++ {
		latest = namedLogin(t, h, "alice", "test-password-123")
		if i == 0 {
			first = latest
		}
	}
	if w := accountRequest(t, h, "GET", "/api/trash", nil, first); w.Code != 401 {
		t.Fatal("oldest member session was retained", w.Code)
	}
	for _, cookie := range []*http.Cookie{latest, bobCookie, admin} {
		if w := accountRequest(t, h, "GET", "/api/trash", nil, cookie); w.Code != 204 {
			t.Fatal("another account's session was evicted", w.Code)
		}
	}
	// The member's quota must never prevent a fresh administrator login.
	login(t, h, password)
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.sessions) != maxSessionsPerUser+3 || len(m.sessionUsers) != len(m.sessions) {
		t.Fatal("sessions grew beyond account quotas", len(m.sessions), len(m.sessionUsers))
	}
}

func TestSuccessfulMemberLoginCannotResetAdministratorFailures(t *testing.T) {
	m, password, h := testManager(t)
	admin := login(t, h, password)
	alice := createAccount(t, h, admin, "alice")
	fastAccountHash(t, m, alice.ID, "test-password-123")
	for i := 0; i < maxAccountAttempts; i++ {
		namedLogin(t, h, "alice", "test-password-123")
		w := accountRequest(t, h, "POST", "/api/auth/session", map[string]string{"username": " ADMIN ", "password": "incorrect-password"}, nil)
		if w.Code != 401 {
			t.Fatal(i, w.Code, w.Body.String())
		}
	}
	namedLogin(t, h, "alice", "test-password-123")
	w := accountRequest(t, h, "POST", "/api/auth/session", map[string]string{"username": "admin", "password": password}, nil)
	if w.Code != 429 {
		t.Fatal("member cleared the administrator's failures", w.Code)
	}
	if w := accountRequest(t, h, "GET", "/api/trash", nil, admin); w.Code != 204 {
		t.Fatal("login failures invalidated an existing session", w.Code)
	}
}

func TestLoginReplacesOnlyTheSameBrowsersUserSession(t *testing.T) {
	m, password, h := testManager(t)
	admin := login(t, h, password)
	user := createAccount(t, h, admin, "alice")
	fastAccountHash(t, m, user.ID, "test-password-123")
	cookie := namedLogin(t, h, "alice", "test-password-123")
	for i := 0; i < 3; i++ {
		w := accountRequest(t, h, "POST", "/api/auth/session", map[string]string{"username": "alice", "password": "test-password-123"}, cookie)
		if w.Code != 200 || len(w.Result().Cookies()) != 1 {
			t.Fatal(w.Code, w.Body.String())
		}
		if old := accountRequest(t, h, "GET", "/api/trash", nil, cookie); old.Code != 401 {
			t.Fatal("re-authentication left the old cookie valid", old.Code)
		}
		cookie = w.Result().Cookies()[0]
	}
	if len(m.sessions) != 2 || len(m.sessionUsers) != 2 {
		t.Fatal("browser re-authentication leaked sessions", len(m.sessions))
	}
}

func TestSuccessfulLoginsCannotResetTheHostWorkBudget(t *testing.T) {
	m, password, h := testManager(t)
	admin := login(t, h, password)
	user := createAccount(t, h, admin, "alice")
	fastAccountHash(t, m, user.ID, "test-password-123")
	for i := 1; i < maxHostAttempts; i++ {
		namedLogin(t, h, "alice", "test-password-123")
	}
	w := accountRequest(t, h, "POST", "/api/auth/session", map[string]string{"username": "unknown-user", "password": "incorrect-password"}, nil)
	if w.Code != 429 {
		t.Fatal("successful logins allowed unlimited work from one address", w.Code)
	}
}
