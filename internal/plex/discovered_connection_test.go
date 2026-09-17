package plex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"mistervision/internal/connection"
	"mistervision/internal/serverstate"
)

type discoveryFixture struct {
	connector                          Connector
	account                            *Client
	server                             connection.Server
	accountCalls, pinCalls, mediaCalls atomic.Int32
	rejectAccount                      atomic.Bool
	failAccount                        atomic.Bool
	denyMedia                          atomic.Bool
}

func newDiscoveryFixture(t *testing.T, linked bool) *discoveryFixture {
	t.Helper()
	f := &discoveryFixture{connector: Connector{StateDir: t.TempDir(), Version: "test"}}
	media := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/identity":
			if r.Header.Get("X-Plex-Token") != "" {
				t.Error("identity received credentials")
			}
			fmt.Fprint(w, `{"MediaContainer":{"machineIdentifier":"server-id"}}`)
		case "/library/sections":
			f.mediaCalls.Add(1)
			if r.Header.Get("X-Plex-Token") != "server-token" {
				t.Error("media received account token or wrong grant")
				w.WriteHeader(401)
				return
			}
			if f.denyMedia.Load() {
				w.WriteHeader(403)
				return
			}
			fmt.Fprint(w, `{"MediaContainer":{"Directory":[]}}`)
		default:
			t.Error("unexpected media request")
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(media.Close)
	f.server = connection.Server{Name: "Home Plex", ID: "server-id", URL: media.URL}
	account := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.accountCalls.Add(1)
		if f.failAccount.Load() {
			w.WriteHeader(503)
			return
		}
		switch r.URL.Path {
		case "/api/v2/pins":
			f.pinCalls.Add(1)
			fmt.Fprint(w, `{"id":2,"code":"ABCD","expiresIn":30}`)
		case "/api/v2/pins/2":
			fmt.Fprint(w, `{"id":2,"code":"ABCD","expiresIn":30,"authToken":"account-token"}`)
		case "/api/v2/user":
			if f.rejectAccount.Load() && r.Header.Get("X-Plex-Token") == "expired" {
				w.WriteHeader(401)
				return
			}
			if r.Header.Get("X-Plex-Token") != "account-token" {
				t.Error("wrong account credential")
			}
			fmt.Fprint(w, `{"id":7,"username":"tester","friendlyName":"Test Viewer"}`)
		case "/api/v2/resources":
			if r.Header.Get("X-Plex-Token") != "account-token" {
				t.Error("wrong resource credential")
			}
			json.NewEncoder(w).Encode([]accountResource{{Name: f.server.Name, ID: f.server.ID, Provides: "server", Token: "server-token", Connections: []resourceConnection{{URI: f.server.URL, Local: true}}}})
		default:
			t.Error("unexpected account request")
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(account.Close)
	f.account = NewClient(Config{}, serverstate.Session{})
	f.account.accountURL = account.URL
	if linked {
		session := serverstate.Session{Server: account.URL, DeviceID: "device", Token: "account-token", UserID: "7"}
		if err := serverstate.SaveSession(filepath.Join(StateDir(f.connector.StateDir), "account"), session); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

func chooseFirst(ctx context.Context, servers []connection.Server) (connection.Server, error) {
	return servers[0], nil
}

func TestPlexDiscoveryLinksChoosesAndReopensWithoutAccountService(t *testing.T) {
	f := newDiscoveryFixture(t, false)
	var code, name bool
	result, err := f.connector.connectDiscovered(t.Context(), connection.Interaction{ChooseServer: chooseFirst, Progress: func(p connection.Presentation) {
		if p.Kind == connection.SetupApproval {
			code = p.Code == "ABCD"
		}
		if p.Kind == connection.SetupServers {
			name = strings.Contains(p.Message, "Test Viewer")
		}
		if strings.Contains(fmt.Sprint(p), "-token") {
			t.Error("credentials reached UI")
		}
	}}, &serverDiscovery{account: f.account})
	if err != nil || result.Server == nil || !code || !name || f.pinCalls.Load() != 1 {
		t.Fatalf("discovery did not finish: %v code=%t name=%t", err, code, name)
	}
	client := result.Server.(*Client)
	if client.Session.Token != "server-token" || client.Session.UserID != "7" {
		t.Fatal("wrong playback session")
	}
	path := filepath.Join(StateDir(f.connector.StateDir), "server.json")
	saved, err := serverstate.LoadServer(path)
	if err != nil || saved != f.server {
		t.Fatal("selection not remembered")
	}
	accountCalls := f.accountCalls.Load()
	f.failAccount.Store(true)
	reopened, err := f.connector.Connect(t.Context(), connection.Interaction{ChooseServer: func(context.Context, []connection.Server) (connection.Server, error) {
		t.Fatal("saved connection reopened picker")
		return connection.Server{}, nil
	}})
	if err != nil || reopened.Server.Identity() != result.Server.Identity() || f.accountCalls.Load() != accountCalls {
		t.Fatalf("reopen requires account service: %v", err)
	}
}

func TestPlexSelectionCancellationAndFailurePreserveWorkingChoice(t *testing.T) {
	for _, failure := range []string{"cancel", "forged choice", "permission denied", "canceled after choice"} {
		t.Run(failure, func(t *testing.T) {
			f := newDiscoveryFixture(t, true)
			_, err := f.connector.connectDiscovered(t.Context(), connection.Interaction{ChooseServer: chooseFirst}, &serverDiscovery{account: f.account})
			if err != nil {
				t.Fatal(err)
			}
			dir := StateDir(f.connector.StateDir)
			paths := []string{filepath.Join(dir, "server.json"), filepath.Join(discoveredSessionDir(dir, f.server), "session.json")}
			before := make([][]byte, len(paths))
			for i, path := range paths {
				before[i], _ = os.ReadFile(path)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			_, err = f.connector.connectDiscovered(ctx, connection.Interaction{SelectServer: true, ChooseServer: func(ctx context.Context, servers []connection.Server) (connection.Server, error) {
				switch failure {
				case "cancel":
					return connection.Server{}, context.Canceled
				case "forged choice":
					s := servers[0]
					s.URL += "/not-offered"
					return s, nil
				case "permission denied":
					f.denyMedia.Store(true)
				case "canceled after choice":
					cancel()
				}
				return servers[0], nil
			}}, &serverDiscovery{account: f.account})
			if err == nil {
				t.Fatal("failed selection succeeded")
			}
			for i, path := range paths {
				after, e := os.ReadFile(path)
				if e != nil || string(after) != string(before[i]) {
					t.Fatal("working choice or credentials replaced")
				}
			}
		})
	}
}

func TestPlexAccountOutagePreservesTokenAndRejectionRelinks(t *testing.T) {
	f := newDiscoveryFixture(t, true)
	path := filepath.Join(StateDir(f.connector.StateDir), "account", "session.json")
	before, _ := os.ReadFile(path)
	f.failAccount.Store(true)
	_, err := f.connector.connectDiscovered(t.Context(), connection.Interaction{SelectServer: true, ChooseServer: chooseFirst}, &serverDiscovery{account: f.account})
	after, _ := os.ReadFile(path)
	if err == nil || string(before) != string(after) || f.pinCalls.Load() != 0 {
		t.Fatal("outage discarded account")
	}
	f.failAccount.Store(false)
	f.rejectAccount.Store(true)
	var saved serverstate.Session
	json.Unmarshal(before, &saved)
	saved.Token = "expired"
	if err := serverstate.SaveSession(filepath.Dir(path), saved); err != nil {
		t.Fatal(err)
	}
	_, err = f.connector.connectDiscovered(t.Context(), connection.Interaction{SelectServer: true, ChooseServer: chooseFirst}, &serverDiscovery{account: f.account})
	if err != nil || f.pinCalls.Load() != 1 {
		t.Fatalf("rejected account did not relink: %v", err)
	}
}

func TestPlexDiscoveryCannotReplaceExplicitConfiguration(t *testing.T) {
	f := newDiscoveryFixture(t, true)
	c := f.connector
	c.Config.Server = "invalid"
	if _, err := c.Connect(t.Context(), connection.Interaction{SelectServer: true}); !errors.Is(err, errServerURL) || f.accountCalls.Load() != 0 {
		t.Fatal("invalid explicit configuration fell back to discovery")
	}
}

func TestFailedSelectionSavePreservesPreviousServerCredentials(t *testing.T) {
	f := newDiscoveryFixture(t, true)
	_, err := f.connector.connectDiscovered(t.Context(), connection.Interaction{ChooseServer: chooseFirst}, &serverDiscovery{account: f.account})
	if err != nil {
		t.Fatal(err)
	}
	dir := StateDir(f.connector.StateDir)
	path := filepath.Join(dir, "server.json")
	selection, _ := os.ReadFile(path)
	credentials := filepath.Join(discoveredSessionDir(dir, f.server), "session.json")
	before, _ := os.ReadFile(credentials)
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/identity" {
			fmt.Fprint(w, `{"MediaContainer":{"machineIdentifier":"server-id"}}`)
			return
		}
		fmt.Fprint(w, `{"MediaContainer":{"Directory":[]}}`)
	}))
	defer other.Close()
	f.server.URL = other.URL
	_, err = f.connector.connectDiscovered(t.Context(), connection.Interaction{SelectServer: true, ChooseServer: func(ctx context.Context, servers []connection.Server) (connection.Server, error) {
		// Make atomic publication fail after the new server authenticates.
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
		return servers[0], nil
	}}, &serverDiscovery{account: f.account})
	if !errors.Is(err, ErrSessionSave) {
		t.Fatalf("save failure lost: %v", err)
	}
	after, _ := os.ReadFile(credentials)
	if string(before) != string(after) {
		t.Fatal("failed selection replaced previous credentials")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, selection, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := f.connector.connectDiscovered(t.Context(), connection.Interaction{}, &serverDiscovery{account: f.account}); err != nil {
		t.Fatalf("previous connection could not reopen: %v", err)
	}
}
