package localauth

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/IceWhaleTech/CasaOS/internal/safefs"
	"github.com/IceWhaleTech/CasaOS/internal/userctx"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

type User = userctx.User

type userRecord struct {
	User
	Hash string `json:"passwordHash"`
}

type Identity = userctx.Identity

func CurrentUser(r *http.Request) (Identity, bool) {
	return userctx.From(r.Context())
}

var usernamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{2,31}$`)

func (m *Manager) loadUsers() error {
	input, err := os.Open(filepath.Join(m.dir, "users.json"))
	if os.IsNotExist(err) {
		m.users = map[string]userRecord{"admin": {User: User{ID: "admin", Username: "admin", Admin: true, Created: time.Now().UTC()}, Hash: string(m.hash)}}
		return m.saveUsersLocked(m.users)
	}
	if err != nil {
		return err
	}
	defer input.Close()
	var records []userRecord
	if err := json.NewDecoder(io.LimitReader(input, 2<<20)).Decode(&records); err != nil {
		return err
	}
	if len(records) == 0 || len(records) > 1000 {
		return errors.New("invalid local account database")
	}
	m.users = map[string]userRecord{}
	names := map[string]bool{}
	admins := 0
	for _, record := range records {
		if record.ID != "admin" {
			if parsed, err := uuid.Parse(record.ID); err != nil || parsed.String() != record.ID {
				return errors.New("invalid local account ID")
			}
		}
		if !usernamePattern.MatchString(record.Username) || names[record.Username] || m.users[record.ID].ID != "" {
			return errors.New("invalid or duplicate local account")
		}
		if _, err := bcrypt.Cost([]byte(record.Hash)); err != nil {
			return err
		}
		if record.Admin {
			if record.ID != "admin" || record.Disabled {
				return errors.New("invalid administrator account")
			}
			admins++
		}
		m.users[record.ID] = record
		names[record.Username] = true
	}
	if admins != 1 {
		return errors.New("one local administrator is required")
	}
	m.hash = []byte(m.users["admin"].Hash)
	return nil
}

func (m *Manager) saveUsersLocked(users map[string]userRecord) error {
	records := make([]userRecord, 0, len(users))
	for _, record := range users {
		records = append(records, record)
	}
	sort.Slice(records, func(i, j int) bool { return records[i].ID < records[j].ID })
	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return err
	}
	return safefs.WriteAtomic(filepath.Join(m.dir, "users.json"), data, 0600)
}

func (m *Manager) cloneUsersLocked() map[string]userRecord {
	users := make(map[string]userRecord, len(m.users))
	for id, record := range m.users {
		users[id] = record
	}
	return users
}

func (m *Manager) identityLocked(r *http.Request) (Identity, bool) {
	if !m.validLocked(r) {
		return Identity{}, false
	}
	c, _ := r.Cookie(cookieName)
	id := m.sessionUsers[c.Value]
	record, ok := m.users[id]
	if !ok || record.Disabled {
		return Identity{}, false
	}
	home := ""
	if m.storageRoot != "" {
		home = filepath.Join(m.storageRoot, id)
	}
	return Identity{User: record.User, Home: home}, true
}

func (m *Manager) invalidateUserLocked(id string) {
	for token, owner := range m.sessionUsers {
		if owner == id {
			delete(m.sessionUsers, token)
			delete(m.sessions, token)
		}
	}
}

func (m *Manager) manageUsers(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	actor, ok := m.identityLocked(r)
	m.mu.Unlock()
	if !ok {
		writeJSON(w, 401, map[string]string{"error": "Authentication required"})
		return
	}
	if !actor.Admin {
		writeJSON(w, 403, map[string]string{"error": "Administrator access required"})
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/users")
	if id == "" && r.Method == http.MethodGet {
		m.mu.Lock()
		users := make([]User, 0, len(m.users))
		for _, record := range m.users {
			users = append(users, record.User)
		}
		m.mu.Unlock()
		sort.Slice(users, func(i, j int) bool { return users[i].Username < users[j].Username })
		writeJSON(w, 200, map[string]interface{}{"users": users})
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Disabled *bool  `json:"disabled"`
	}
	if r.Method != http.MethodDelete {
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
			writeJSON(w, 400, map[string]string{"error": "Invalid account request"})
			return
		}
	}
	if body.Password != "" && (len(body.Password) < 12 || len(body.Password) > 72) {
		writeJSON(w, 400, map[string]string{"error": "Password must contain 12 to 72 bytes"})
		return
	}
	var hash []byte
	if body.Password != "" {
		var err error
		hash, err = bcrypt.GenerateFromPassword([]byte(body.Password), bcrypt.DefaultCost)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": "Unable to hash password"})
			return
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	// Account changes must not outlive the administrator session authorizing them.
	actor, ok = m.identityLocked(r)
	if !ok || !actor.Admin {
		writeJSON(w, 401, map[string]string{"error": "Authentication required"})
		return
	}
	users := m.cloneUsersLocked()
	var record userRecord
	if id == "" && r.Method == http.MethodPost {
		name := strings.ToLower(strings.TrimSpace(body.Username))
		if !usernamePattern.MatchString(name) || len(hash) == 0 || len(users) >= 1000 {
			writeJSON(w, 400, map[string]string{"error": "Use a unique username (3-32 letters, digits, _, . or -) and a password of 12-72 bytes"})
			return
		}
		for _, existing := range users {
			if existing.Username == name {
				writeJSON(w, 409, map[string]string{"error": "Username already exists"})
				return
			}
		}
		record = userRecord{User: User{ID: uuid.NewString(), Username: name, Created: time.Now().UTC()}, Hash: string(hash)}
		if m.storageRoot != "" {
			if err := m.prepareHome(record.ID); err != nil {
				writeJSON(w, 500, map[string]string{"error": "Unable to create protected user storage"})
				return
			}
		}
	} else if strings.HasPrefix(id, "/") && (r.Method == http.MethodPatch || r.Method == http.MethodDelete) {
		id = strings.TrimPrefix(id, "/")
		var found bool
		record, found = users[id]
		if !found {
			writeJSON(w, 404, map[string]string{"error": "Account not found"})
			return
		}
		if record.Admin {
			writeJSON(w, 400, map[string]string{"error": "Use the password settings to change the administrator password"})
			return
		}
		if len(hash) > 0 {
			record.Hash = string(hash)
		}
		if body.Disabled != nil {
			record.Disabled = *body.Disabled
		}
		if r.Method == http.MethodDelete {
			record.Disabled = true
		}
	} else {
		writeJSON(w, 405, map[string]string{"error": "Method not allowed"})
		return
	}
	users[record.ID] = record
	if err := m.saveUsersLocked(users); err != nil {
		writeJSON(w, 500, map[string]string{"error": "Unable to save accounts"})
		return
	}
	m.users = users
	m.invalidateUserLocked(record.ID)
	writeJSON(w, 200, map[string]interface{}{"success": true, "user": record.User})
}
