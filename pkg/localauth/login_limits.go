package localauth

import "time"

// Caller holds Manager.mu. Both tables stay bounded, including unknown accounts.
func reserveAttempt(table map[string]attempt, key string, limit int, now time.Time) bool {
	for existing, a := range table {
		if !now.Before(a.until) {
			delete(table, existing)
		}
	}
	a, known := table[key]
	if a.count >= limit {
		return false
	}
	if !known && len(table) >= maxAttemptHosts {
		var oldest string
		var last time.Time
		for existing, candidate := range table {
			if oldest == "" || candidate.last.Before(last) {
				oldest, last = existing, candidate.last
			}
		}
		delete(table, oldest)
	}
	if !known {
		a.until = now.Add(15 * time.Minute)
	}
	a.count++
	a.last = now
	table[key] = a
	return true
}

func (m *Manager) makeSessionRoomLocked(userID string) {
	for {
		count := 0
		var oldest string
		var expiry time.Time
		for token, owner := range m.sessionUsers {
			if owner == userID {
				count++
				if oldest == "" || m.sessions[token].Before(expiry) || m.sessions[token].Equal(expiry) && token < oldest {
					oldest, expiry = token, m.sessions[token]
				}
			}
		}
		if count < maxSessionsPerUser {
			return
		}
		delete(m.sessions, oldest)
		delete(m.sessionUsers, oldest)
	}
}
