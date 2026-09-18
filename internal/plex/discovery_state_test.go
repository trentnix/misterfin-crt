package plex

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mistervision/internal/connection"
	"mistervision/internal/serverstate"
)

func TestDiscoveryStateAtomicStorageAndPublicProjection(t *testing.T) {
	dir := t.TempDir()
	server := connection.Server{ID: "one", Name: "Home", URL: "https://server.example"}
	account := serverstate.Session{Server: "https://plex.tv", DeviceID: "device", UserID: "7", Token: "account-token"}
	credentials := serverstate.Session{Server: server.URL, ServerID: server.ID, DeviceID: account.DeviceID, UserID: account.UserID, Token: "server-token"}
	state := discoveryState{Server: server, Account: &account, Credentials: &credentials}
	if err := saveDiscoveryState(dir, state); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "server.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("credentials are not private")
	}
	public, err := serverstate.LoadServer(path)
	if err != nil || public != server {
		t.Fatal("shared catalog cannot read public server metadata")
	}
	loaded, err := loadDiscoveryState(dir)
	if err != nil || *loaded.Account != account || *loaded.Credentials != credentials {
		t.Fatal("credentials did not round-trip")
	}
	for _, invalid := range []discoveryState{
		{Server: server, Account: &account},
		{Server: server, Credentials: &credentials},
		{Server: connection.Server{ID: "wrong", Name: server.Name, URL: server.URL}, Account: &account, Credentials: &credentials},
		{Server: connection.Server{ID: server.ID, Name: strings.Repeat("x", 16384), URL: server.URL}, Account: &account, Credentials: &credentials},
	} {
		if !errors.Is(saveDiscoveryState(dir, invalid), ErrSessionSave) {
			t.Fatal("invalid replacement accepted")
		}
		after, err := os.ReadFile(path)
		if err != nil || string(before) != string(after) {
			t.Fatal("failed replacement changed committed credentials")
		}
	}
	if err := os.WriteFile(path, []byte(`{"ID":"one","Name":"Home","URL":"https://server.example","account":{}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadDiscoveryAccount(dir, account.Server); !errors.Is(err, ErrSessionSave) {
		t.Fatal("damaged state silently fell back to another account")
	}
}

func TestDiscoveryStateRejectsIncompleteProfiles(t *testing.T) {
	for _, tc := range []struct {
		name    string
		profile *connection.Profile
		checked bool
	}{
		{"profile", &connection.Profile{ID: "7", Name: "Parent"}, true},
		{"unchecked profile", &connection.Profile{ID: "7", Name: "Parent"}, false},
		{"checked without credentials", nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newDiscoveryFixture(t, true)
			state := discoveryState{Server: f.server, Profile: tc.profile, HomeChecked: tc.checked}
			data, err := json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(StateDir(f.connector.StateDir), "server.json")
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := loadDiscoveryState(filepath.Dir(path)); !errors.Is(err, ErrSessionSave) {
				t.Fatal("partial profile record accepted")
			}
			_, err = f.connector.connectDiscovered(t.Context(), connection.Interaction{SelectServer: true, ChooseServer: chooseFirst}, &serverDiscovery{account: f.account})
			if !errors.Is(err, ErrSessionSave) || f.accountCalls.Load() != 0 {
				t.Fatal("invalid profile record reached authentication")
			}
			after, err := os.ReadFile(path)
			if err != nil || string(after) != string(data) {
				t.Fatal("invalid state was not preserved")
			}
		})
	}
	// Genuine legacy metadata-only records remain readable.
	dir := t.TempDir()
	server := connection.Server{ID: "legacy", Name: "Legacy", URL: "http://server"}
	if err := serverstate.SaveServer(filepath.Join(dir, "server.json"), server); err != nil {
		t.Fatal(err)
	}
	state, err := loadDiscoveryState(dir)
	if err != nil || state.Server != server {
		t.Fatal("legacy metadata-only record rejected")
	}
}
