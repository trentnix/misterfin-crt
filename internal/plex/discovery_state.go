package plex

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"

	"mistervision/internal/connection"
	"mistervision/internal/serverstate"
)

// discoveryState commits the selected server and its account together. The
// shared catalog reads only the embedded public Server. Credentials stay private
// to this adapter and the mode-0600 state file, never in picker presentations.
type discoveryState struct {
	connection.Server
	Account     *serverstate.Session `json:"account,omitempty"`
	Credentials *serverstate.Session `json:"credentials,omitempty"`
	HomeChecked bool                 `json:"home_checked,omitempty"`
	Profile     *connection.Profile  `json:"profile,omitempty"`
}

// loadDiscoveryState accepts the original metadata-only server.json as well as
// the complete record. Legacy account and endpoint credentials are read lazily.
func loadDiscoveryState(dir string) (discoveryState, error) {
	var state discoveryState
	f, err := os.Open(filepath.Join(dir, "server.json"))
	if err != nil {
		return state, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return state, ErrSessionSave
	}
	data, err := io.ReadAll(io.LimitReader(f, 16385))
	if err != nil || len(data) > 16384 || json.Unmarshal(data, &state) != nil {
		return state, ErrSessionSave
	}
	return state, state.validate()
}

// validate rejects partial or mismatched credentials instead of silently
// falling back to a different account after state damage.
func (s discoveryState) validate() error {
	if err := s.Server.Validate(); err != nil {
		return ErrSessionSave
	}
	if s.Account == nil && s.Credentials == nil {
		return nil
	}
	if s.Account == nil || s.Credentials == nil {
		return ErrSessionSave
	}
	a, c := s.Account, s.Credentials
	viewer := a.UserID
	if s.Profile != nil {
		if s.Profile.ID == "" || s.Profile.Name == "" || !s.HomeChecked {
			return ErrSessionSave
		}
		viewer = s.Profile.ID
	}
	if a.Token == "" || a.UserID == "" || a.DeviceID == "" || c.Token == "" || c.Server != s.URL || c.ServerID != s.ID || c.UserID != viewer || c.DeviceID != a.DeviceID {
		return ErrSessionSave
	}
	return nil
}

// saveDiscoveryState atomically publishes a complete replacement. A failed
// write leaves both the old account and server available together.
func saveDiscoveryState(dir string, s discoveryState) error {
	if err := s.validate(); err != nil || s.Account == nil {
		return ErrSessionSave
	}
	data, err := json.Marshal(s)
	if err != nil || len(data)+1 > 16384 {
		return ErrSessionSave
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return ErrSessionSave
	}
	f, err := os.CreateTemp(dir, ".plex-connection-*")
	if err != nil {
		return ErrSessionSave
	}
	defer os.Remove(f.Name())
	_, err = f.Write(append(data, '\n'))
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(f.Name(), filepath.Join(dir, "server.json"))
	}
	if err != nil {
		return ErrSessionSave
	}
	return nil
}

// loadDiscoveryAccount prefers the committed account. Before the first
// complete selection, it reads the legacy account record for compatibility.
func loadDiscoveryAccount(dir, origin string) (serverstate.Session, bool, error) {
	state, err := loadDiscoveryState(dir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return serverstate.Session{}, false, ErrSessionSave
	}
	if err == nil && state.Account != nil {
		if state.Account.Server != origin {
			return serverstate.Session{}, false, ErrSessionSave
		}
		return *state.Account, false, nil
	}
	return serverstate.LoadSession(filepath.Join(dir, "account"), origin)
}
