package connection

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"

	"mistervision/internal/jellyfin"
	"mistervision/internal/serverstate"
)

// savedUser keeps one independently authorized user. Session is private and
// must never cross the presentation boundary. Each user retains a device ID.
type savedUser struct {
	User    jellyfin.User    `json:"user"`
	Session jellyfin.Session `json:"session"`
}

// userStore identifies the sign-ins allowed on this device. session.json chooses
// the active user, but cannot restore credentials removed from an existing roster.
// Callers serialize reads and writes with the connector's other state changes.
type userStore []savedUser

const maxUserStateBytes = 1 << 20

var errUserState = errors.New("invalid saved Jellyfin users")

// loadUsers preserves malformed or unreadable data and reports a storage error.
func loadUsers(dir string) (userStore, error) {
	path := filepath.Join(dir, "jellyfin-users.json")
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxUserStateBytes {
		return nil, errUserState
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxUserStateBytes+1))
	if err != nil {
		return nil, err
	}
	var users userStore
	if len(data) > maxUserStateBytes || json.Unmarshal(data, &users) != nil {
		return nil, errUserState
	}
	if err := users.validate(); err != nil {
		return nil, err
	}
	// A present empty roster is authoritative. Nil means no roster existed and
	// permits migration of an older active session.
	if users == nil {
		users = userStore{}
	}
	return users, nil
}

// find returns an identity only when it belongs to the current server.
func (users userStore) find(server jellyfin.Session, id string) (savedUser, bool) {
	for _, user := range users {
		if user.User.ID == id && sameUserServer(user.Session, server) {
			return user, true
		}
	}
	return savedUser{}, false
}

// without copies the roster while removing only the selected server's identity.
func (users userStore) without(server jellyfin.Session, id string) userStore {
	remaining := make(userStore, 0, len(users))
	for _, user := range users {
		if user.User.ID != id || !sameUserServer(user.Session, server) {
			remaining = append(remaining, user)
		}
	}
	return remaining
}

// validate bounds the roster and rejects incomplete or duplicate credentials.
func (users userStore) validate() error {
	if len(users) > 64 {
		return errUserState
	}
	for i, u := range users {
		s := u.Session
		if u.User.ID == "" || u.User.ID != s.UserID || u.User.Name == "" || s.Token == "" || s.DeviceID == "" || s.Server == "" {
			return errUserState
		}
		for _, previous := range users[:i] {
			if previous.User.ID == u.User.ID && sameUserServer(previous.Session, s) {
				return errUserState
			}
		}
	}
	return nil
}

// profileCount counts only users bound to the current server.
func (users userStore) profileCount(session jellyfin.Session) int {
	count := 0
	for _, user := range users {
		if sameUserServer(user.Session, session) {
			count++
		}
	}
	return count
}

// remember updates an authenticated identity and reports whether storage changed.
func (users *userStore) remember(user savedUser) bool {
	for i, previous := range *users {
		if previous.User.ID == user.User.ID && sameUserServer(previous.Session, user.Session) {
			changed := (*users)[i] != user
			(*users)[i] = user
			return changed
		}
	}
	*users = append(*users, user)
	return true
}

// save atomically replaces private storage. It never changes the active session.
func (users userStore) save(dir string) error {
	if err := users.validate(); err != nil {
		return err
	}
	data, err := json.Marshal(users)
	if err != nil {
		return err
	}
	if len(data) > maxUserStateBytes {
		return errUserState
	}
	return serverstate.WriteFile(filepath.Join(dir, "jellyfin-users.json"), data)
}

// sameUserServer follows the session store's stable-ID-first binding. An address
// change still requires public identity verification before using any token.
func sameUserServer(a, b jellyfin.Session) bool {
	if a.ServerID != "" && b.ServerID != "" {
		return a.ServerID == b.ServerID
	}
	return a.Server == b.Server
}
