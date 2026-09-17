package plex

import (
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
