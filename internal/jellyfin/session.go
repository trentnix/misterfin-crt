package jellyfin

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

// Session belongs only to the Go client and is bound to its server URL.
type Session struct{ Server, DeviceID, Token, UserID string }

// LoadSession restores sign-in for server or creates a new device identity.
// A malformed or oversized record is preserved privately as session-damaged-*
// before replacement. recovered reports that case so callers can explain the
// fresh sign-in. I/O and permission errors preserve the original and return an
// error. Session records are limited to 64 KiB. Calls for one directory must be
// serialized with SaveSession.
func LoadSession(dir, server string) (session Session, recovered bool, err error) {
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
		} else if saved.Server == server && saved.DeviceID != "" {
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
	return session, recovered, SaveSession(dir, session)
}

// SaveSession atomically replaces sign-in data using a private file. The caller
// must serialize writes and LoadSession calls for this directory. Errors leave
// the previous record intact. Credentials must never be included in diagnostics.
func SaveSession(dir string, s Session) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".session-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	err = json.NewEncoder(f).Encode(s)
	if e := f.Close(); err == nil {
		err = e
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), filepath.Join(dir, "session.json"))
}
