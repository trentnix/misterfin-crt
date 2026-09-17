package plex

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mistervision/internal/connection"
	"mistervision/internal/media"
	"mistervision/internal/serverstate"
)

func TestConnectorKeepsAccountsSeparate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/identity" {
			fmt.Fprint(w, `{"MediaContainer":{"machineIdentifier":"server-id"}}`)
			return
		}
		if r.Header.Get("X-Plex-Token") != "plex-private" {
			t.Error("wrong account credential")
			w.WriteHeader(401)
			return
		}
		fmt.Fprint(w, `{"MediaContainer":{"Directory":[]}}`)
	}))
	defer server.Close()
	root := t.TempDir()
	jellyfin := serverstate.Session{Server: server.URL, DeviceID: "jellyfin-device", Token: "jellyfin-private", UserID: "7"}
	plex := serverstate.Session{Server: server.URL, DeviceID: "plex-device", Token: "plex-private", UserID: "7"}
	if err := serverstate.SaveSession(root, jellyfin); err != nil {
		t.Fatal(err)
	}
	if err := serverstate.SaveSession(StateDir(root), plex); err != nil {
		t.Fatal(err)
	}
	account := plex
	account.Server = "https://plex.tv"
	plex.ServerID = "server-id"
	if err := saveDiscoveryState(StateDir(root), discoveryState{Server: connection.Server{ID: "server-id", Name: "Test", URL: server.URL}, Account: &account, Credentials: &plex, HomeChecked: true}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(root, "session.json"))
	if err != nil {
		t.Fatal(err)
	}
	c := Connector{Config: Config{Server: server.URL + "/"}, StateDir: root, Version: "test"}
	session, err := c.Connect(t.Context(), connection.Interaction{Progress: func(p connection.Presentation) {
		if p.Kind == connection.SetupApproval {
			t.Error("saved sign-in requested approval")
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	if session.Remote != nil || session.Server.Identity().Server != "plex:"+server.URL || session.Server.Identity().User != "7" {
		t.Fatal("wrong session services or identity")
	}
	after, err := os.ReadFile(filepath.Join(root, "session.json"))
	if err != nil || string(before) != string(after) {
		t.Fatal("Jellyfin sign-in changed")
	}
}

func TestServerURLValidation(t *testing.T) {
	for _, raw := range []string{"", "ftp://server", "http:///missing", "http://user:private@server", "http://server?token=private", "http://server#private"} {
		if _, err := serverURL(raw); !errors.Is(err, errServerURL) || strings.Contains(err.Error(), "private") {
			t.Errorf("unsafe URL accepted or exposed: %q", raw)
		}
	}
	for _, raw := range []string{"http://server:32400", "https://server/plex/"} {
		if _, err := serverURL(raw); err != nil {
			t.Fatal(err)
		}
	}
}

func TestConnectionPresentationsNeverExposeErrors(t *testing.T) {
	c := Connector{StateDir: t.TempDir()}
	for _, err := range []error{errors.New("private-token"), fmt.Errorf("private-token: %w", ErrSessionSave), fmt.Errorf("private-token: %w", media.ErrUnauthorized), ErrCodeExpired, errServerURL} {
		p := c.Describe(err)
		if p.Kind != connection.SetupFailure || p.Retry == "" || strings.Contains(fmt.Sprint(p), "private-token") {
			t.Fatalf("unsafe failure presentation: %+v", p)
		}
	}
}
