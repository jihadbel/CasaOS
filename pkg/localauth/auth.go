// Package localauth provides the Windows standalone server's local administrator login.
package localauth

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/IceWhaleTech/CasaOS/internal/safefs"
	"golang.org/x/crypto/bcrypt"
)

type contextKey struct{}

const cookieName = "casaos_session"
const sessionLife = 12 * time.Hour
const maxAttemptHosts = 1024

type attempt struct {
	count int
	until time.Time
	last  time.Time
}
type Manager struct {
	mu       sync.Mutex
	hash     []byte
	dir      string
	sessions map[string]time.Time
	attempts map[string]attempt
}

func Authenticated(r *http.Request) bool { ok, _ := r.Context().Value(contextKey{}).(bool); return ok }

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// New creates a unique bootstrap password in a directory restricted to its owner,
// Administrators and SYSTEM. The hash is the only persistent login verifier.
func New(dir string) (*Manager, error) {
	if err := safefs.NoLinks(dir); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	if err := protectDirectory(dir); err != nil {
		return nil, err
	}
	m := &Manager{dir: dir, sessions: map[string]time.Time{}, attempts: map[string]attempt{}}
	hashPath := filepath.Join(dir, "password.hash")
	hash, err := os.ReadFile(hashPath)
	if os.IsNotExist(err) {
		password, err := randomToken()
		if err != nil {
			return nil, err
		}
		hash, err = bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			return nil, err
		}
		if err := safefs.WriteAtomic(filepath.Join(dir, "initial-password.txt"), []byte(password+"\n"), 0600); err != nil {
			return nil, err
		}
		if err := safefs.WriteAtomic(hashPath, hash, 0600); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	if _, err := bcrypt.Cost(hash); err != nil {
		return nil, fmt.Errorf("invalid local password hash: %w", err)
	}
	m.hash = hash
	return m, nil
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func sameOrigin(r *http.Request) bool {
	if strings.EqualFold(r.Header.Get("Sec-Fetch-Site"), "cross-site") {
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		return err == nil && u.User == nil && u.Path == "" && u.RawQuery == "" && u.Fragment == "" && strings.EqualFold(u.Host, r.Host) && u.Scheme == requestScheme(r)
	}
	return true
}

// Only a directly connected local proxy may describe the browser's TLS scheme.
// Keep Host unchanged at the proxy; forwarded host/IP headers are not trusted.
func requestScheme(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && net.ParseIP(host).IsLoopback() {
		values := r.Header.Values("X-Forwarded-Proto")
		if len(values) == 1 && strings.EqualFold(strings.TrimSpace(values[0]), "https") {
			return "https"
		}
	}
	return "http"
}

func (m *Manager) valid(r *http.Request) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.validLocked(r)
}

// validLocked rechecks authorization while committing a credential change.
func (m *Manager) validLocked(r *http.Request) bool {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return false
	}
	until, ok := m.sessions[c.Value]
	if !ok || time.Now().After(until) {
		delete(m.sessions, c.Value)
		return false
	}
	return true
}

func (m *Manager) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		if r.URL.Path == "/" && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
			next.ServeHTTP(w, r)
			return
		}
		if !m.valid(r) && r.Method == http.MethodGet && strings.Contains(r.Header.Get("Accept"), "text/html") && (r.URL.Path == "/v1/file" || r.URL.Path == "/v3/file") {
			http.Redirect(w, r, "/?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
			return
		}
		if !sameOrigin(r) {
			writeJSON(w, 403, map[string]string{"error": "Cross-origin requests are not allowed"})
			return
		}
		if r.URL.Path == "/api/auth/session" {
			m.session(w, r)
			return
		}
		if r.URL.Path == "/api/auth/password" {
			m.changePassword(w, r)
			return
		}
		if !m.valid(r) {
			writeJSON(w, 401, map[string]string{"error": "Authentication required"})
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), contextKey{}, true)))
	})
}

func (m *Manager) session(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, 200, map[string]bool{"authenticated": m.valid(r)})
	case http.MethodDelete:
		if c, err := r.Cookie(cookieName); err == nil {
			m.mu.Lock()
			delete(m.sessions, c.Value)
			m.mu.Unlock()
		}
		http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: requestScheme(r) == "https", MaxAge: -1})
		writeJSON(w, 200, map[string]bool{"authenticated": false})
	case http.MethodPost:
		// Bound brute-force work and the number of tracked addresses.
		host, _, _ := net.SplitHostPort(r.RemoteAddr)
		m.mu.Lock()
		now := time.Now()
		for key, a := range m.attempts {
			if now.After(a.until) {
				delete(m.attempts, key)
			}
		}
		a, known := m.attempts[host]
		if a.count >= 10 {
			m.mu.Unlock()
			writeJSON(w, 429, map[string]string{"error": "Try again in 15 minutes"})
			return
		}
		if !known && len(m.attempts) >= maxAttemptHosts {
			// Bound memory without turning one full table into a global lockout.
			var oldest string
			var last time.Time
			for key, candidate := range m.attempts {
				if oldest == "" || candidate.last.Before(last) {
					oldest, last = key, candidate.last
				}
			}
			delete(m.attempts, oldest)
		}
		a.count++
		a.until = now.Add(15 * time.Minute)
		a.last = now
		m.attempts[host] = a
		hash := append([]byte(nil), m.hash...)
		m.mu.Unlock()
		var body struct {
			Password string `json:"password"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil || bcrypt.CompareHashAndPassword(hash, []byte(body.Password)) != nil {
			writeJSON(w, 401, map[string]string{"error": "Invalid password"})
			return
		}
		token, err := randomToken()
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": "Unable to create session"})
			return
		}
		m.mu.Lock()
		// A password rotation invalidates login work already in flight.
		if !bytes.Equal(hash, m.hash) {
			m.mu.Unlock()
			writeJSON(w, 401, map[string]string{"error": "Password changed; sign in again"})
			return
		}
		now = time.Now()
		for key, until := range m.sessions {
			if now.After(until) {
				delete(m.sessions, key)
			}
		}
		if len(m.sessions) >= 1024 {
			m.mu.Unlock()
			writeJSON(w, 503, map[string]string{"error": "Too many sessions"})
			return
		}
		delete(m.attempts, host)
		m.sessions[token] = now.Add(sessionLife)
		m.mu.Unlock()
		http.SetCookie(w, &http.Cookie{Name: cookieName, Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: requestScheme(r) == "https", MaxAge: int(sessionLife.Seconds())})
		writeJSON(w, 200, map[string]bool{"authenticated": true})
	default:
		writeJSON(w, 405, map[string]string{"error": "Method not allowed"})
	}
}

func (m *Manager) changePassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, 405, map[string]string{"error": "Method not allowed"})
		return
	}
	if !m.valid(r) {
		writeJSON(w, 401, map[string]string{"error": "Authentication required"})
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil || len(body.Password) < 12 || len(body.Password) > 72 {
		writeJSON(w, 400, map[string]string{"error": "Password must contain 12 to 72 bytes"})
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(body.Password), bcrypt.DefaultCost)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "Unable to change password"})
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.validLocked(r) {
		writeJSON(w, 401, map[string]string{"error": "Authentication required"})
		return
	}
	if err := safefs.WriteAtomic(filepath.Join(m.dir, "password.hash"), hash, 0600); err != nil {
		writeJSON(w, 500, map[string]string{"error": "Unable to save password"})
		return
	}
	m.hash = hash
	m.sessions = map[string]time.Time{}
	_ = os.Remove(filepath.Join(m.dir, "initial-password.txt"))
	writeJSON(w, 200, map[string]bool{"success": true})
}
