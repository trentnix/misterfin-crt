// Package serverstate persists private sign-in records for media backends.
package serverstate

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Session belongs to one client installation and records its server URL and
// optional stable server identity. Tokens and device identifiers are private.
type Session struct {
	Server, DeviceID, Token, UserID string
	// ServerID optionally binds sign-in to a stable, verified server identity.
	ServerID string `json:",omitempty"`
}

// LoadSession restores sign-in for server or creates an unsaved device identity.
// The caller must save a new session only after successful authentication. A
// different server never replaces the existing record during setup.
// A malformed or oversized record is preserved privately as session-damaged-*.
// recovered reports that case so callers can explain the fresh sign-in.
// I/O and permission errors preserve the original and return an error. Session records are limited to 64 KiB. Calls for one directory must be
// serialized with SaveSession.
func LoadSession(dir, server string) (Session, bool, error) {
	return LoadSessionForServer(dir, server, "")
}

// LoadSessionForServer also accepts a matching stable server ID after an address
// change. The returned Server remains the address recorded with the credentials.
// Before sending them to a different address, the caller must verify that endpoint's
// identity. Empty serverID retains strict URL matching. Storage rules match LoadSession.
func LoadSessionForServer(dir, server, serverID string) (session Session, recovered bool, err error) {
	path := filepath.Join(dir, "session.json")
	f, err := os.Open(path)
	if err == nil {
		const limit = 64 << 10
		info, statErr := f.Stat()
		if statErr != nil || !info.Mode().IsRegular() {
			f.Close()
			return Session{}, false, errors.New("saved sign-in is not a readable regular file")
		}
		data, readErr := io.ReadAll(io.LimitReader(f, limit+1))
		f.Close()
		if readErr != nil {
			return Session{}, false, errors.New("cannot read saved sign-in")
		}
		var saved Session
		if len(data) > limit || json.Unmarshal(data, &saved) != nil {
			// Preserve all bytes, including oversized records, without retaining
			// them in memory. Chmod before renaming keeps credentials private.
			backup, e := os.CreateTemp(dir, "session-damaged-*")
			if e != nil {
				return Session{}, false, e
			}
			name := backup.Name()
			e = backup.Close()
			if e == nil {
				e = os.Chmod(path, 0600)
			}
			if e == nil {
				e = os.Rename(path, name)
			}
			if e != nil {
				os.Remove(name)
				return Session{}, false, e
			}
			recovered = true
		} else if saved.DeviceID != "" && saved.matchesServer(server, serverID) {
			return saved, false, nil
		}
	} else if !os.IsNotExist(err) {
		return Session{}, false, fmt.Errorf("read saved sign-in: %w", err)
	}
	var id [16]byte
	if _, err = rand.Read(id[:]); err != nil {
		return Session{}, recovered, err
	}
	session = Session{Server: server, DeviceID: hex.EncodeToString(id[:])}
	return session, recovered, nil
}

// SaveSession atomically replaces sign-in data using a private file. The caller
// must serialize writes and LoadSession calls for this directory. Errors leave
// the previous record intact. Credentials must never be included in diagnostics.
func SaveSession(dir string, s Session) error {
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return WriteFile(filepath.Join(dir, "session.json"), append(data, '\n'))
}

// matchesServer uses stable identity when both records supply it. Legacy and
// explicitly configured connections retain their existing URL-only matching.
func (s Session) matchesServer(server, id string) bool {
	if id != "" && s.ServerID != "" {
		return s.ServerID == id
	}
	return s.Server == server
}
