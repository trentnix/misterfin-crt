package plex

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mistervision/internal/connection"
	"mistervision/internal/diagnostics"
	"mistervision/internal/serverstate"
)

// rememberedPlex installs an unavailable address alongside a valid linked
// account. The replacement media endpoint belongs to the same server identity.
func rememberedPlex(t *testing.T, secure bool) (*discoveryFixture, connection.Server) {
	t.Helper()
	f := newDiscoveryFixture(t, true)
	oldEndpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/identity" && r.Header.Get("X-Plex-Token") != "" {
			t.Error("failed identity check received credentials")
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(oldEndpoint.Close)
	old := f.server
	old.URL = oldEndpoint.URL
	if secure {
		old.URL = strings.Replace(old.URL, "http://", "https://", 1)
	}
	dir := StateDir(f.connector.StateDir)
	if err := serverstate.SaveServer(filepath.Join(dir, "server.json"), old); err != nil {
		t.Fatal(err)
	}
	if err := serverstate.SaveSession(discoveredSessionDir(dir, old), serverstate.Session{Server: old.URL, ServerID: old.ID, DeviceID: "device", Token: "server-token", UserID: "7"}); err != nil {
		t.Fatal(err)
	}
	return f, old
}

func TestPlexRecoveryConfirmsIdentityAndReopensWithoutAccount(t *testing.T) {
	f, old := rememberedPlex(t, false)
	logPath := filepath.Join(t.TempDir(), "diagnostics.log")
	log, err := diagnostics.Open(diagnostics.Config{Enabled: true, Path: logPath, MaxBytes: 8192})
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	f.connector.Diagnostics = log
	var prompt connection.Presentation
	selected := false
	i := connection.Interaction{
		Progress: func(p connection.Presentation) { prompt = p },
		ChooseServer: func(ctx context.Context, servers []connection.Server) (connection.Server, error) {
			if len(servers) != 1 || servers[0] != f.server || prompt.Title != "Server address changed" || prompt.Message == "" {
				t.Fatalf("unexpected recovery prompt: %+v %+v", prompt, servers)
			}
			if f.mediaCalls.Load() != 0 {
				t.Fatal("new address received credentials before approval")
			}
			selected = true
			return servers[0], nil
		},
	}
	d := &serverDiscovery{account: f.account}
	result, err := f.connector.connectDiscovered(t.Context(), i, d)
	if err != nil || !selected || result.Server == nil {
		t.Fatalf("recovery failed: %v", err)
	}
	client := result.Server.(*Client)
	if client.Session.DeviceID != "device" || client.Session.UserID != "7" || client.Session.Token != "server-token" || client.Session.ServerID != old.ID {
		t.Fatal("account identity changed during recovery")
	}
	saved, err := serverstate.LoadServer(filepath.Join(StateDir(f.connector.StateDir), "server.json"))
	if err != nil || saved != f.server {
		t.Fatal("new address was not remembered")
	}
	calls := f.accountCalls.Load()
	f.failAccount.Store(true)
	if _, err := f.connector.connectDiscovered(t.Context(), connection.Interaction{}, d); err != nil {
		t.Fatalf("saved reopen required account service: %v", err)
	}
	if f.accountCalls.Load() != calls || f.pinCalls.Load() != 0 {
		t.Fatal("reopen refreshed or relinked account")
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(logPath)
	if err != nil || !bytes.Contains(data, []byte("connection.address-recovered")) {
		t.Fatal("recovery not logged")
	}
	for _, private := range []string{old.URL, f.server.URL, "server-token", "account-token"} {
		if bytes.Contains(data, []byte(private)) {
			t.Fatal("recovery log exposed private connection data")
		}
	}
}

func TestPlexRecoveryFailurePreservesRememberedConnection(t *testing.T) {
	for _, failure := range []string{"cancel", "forged choice", "cancel after choice", "account outage", "unrelated server", "identity mismatch", "HTTPS downgrade", "rejected grant", "save failure", "identity changed after choice", "explicit configuration"} {
		t.Run(failure, func(t *testing.T) {
			f, old := rememberedPlex(t, failure == "HTTPS downgrade")
			dir := StateDir(f.connector.StateDir)
			paths := []string{filepath.Join(dir, "server.json"), filepath.Join(discoveredSessionDir(dir, old), "session.json"), filepath.Join(dir, "account", "session.json")}
			before := make([][]byte, len(paths))
			for i, path := range paths {
				var err error
				before[i], err = os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
			}
			if failure == "account outage" {
				f.failAccount.Store(true)
			}
			if failure == "unrelated server" {
				f.server.ID = "unrelated"
			}
			var replacement *httptest.Server
			if failure == "identity mismatch" || failure == "identity changed after choice" {
				replacement = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Header.Get("X-Plex-Token") != "" {
						t.Error("mismatched identity received credentials")
					}
					fmt.Fprint(w, `{"MediaContainer":{"machineIdentifier":"different"}}`)
				}))
				defer replacement.Close()
				if failure == "identity mismatch" {
					f.server.URL = replacement.URL
				}
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			choices := 0
			i := connection.Interaction{ChooseServer: func(ctx context.Context, servers []connection.Server) (connection.Server, error) {
				choices++
				switch failure {
				case "cancel":
					return connection.Server{}, context.Canceled
				case "forged choice":
					return connection.Server{ID: old.ID, Name: "forged", URL: servers[0].URL + "/not-offered"}, nil
				case "cancel after choice":
					cancel()
				case "rejected grant":
					f.denyMedia.Store(true)
				case "save failure":
					if err := os.Remove(paths[0]); err != nil {
						t.Fatal(err)
					}
					if err := os.Mkdir(paths[0], 0700); err != nil {
						t.Fatal(err)
					}
				case "identity changed after choice":
					// The selected address now reports a different server identity.
					f.account.HTTP = &http.Client{Transport: discoveryTransportFunc(func(r *http.Request) (*http.Response, error) {
						request, err := http.NewRequestWithContext(r.Context(), http.MethodGet, replacement.URL+"/identity", nil)
						if err != nil {
							return nil, err
						}
						return http.DefaultTransport.RoundTrip(request)
					})}
				}
				return servers[0], nil
			}}
			var err error
			if failure == "explicit configuration" {
				f.connector.Config.Server = old.URL
				saved, _, loadErr := serverstate.LoadSession(discoveredSessionDir(dir, old), old.URL)
				if loadErr != nil {
					t.Fatal(loadErr)
				}
				if saveErr := serverstate.SaveSession(dir, saved); saveErr != nil {
					t.Fatal(saveErr)
				}
				_, err = f.connector.Connect(ctx, i)
			} else {
				_, err = f.connector.connectDiscovered(ctx, i, &serverDiscovery{account: f.account})
			}
			if err == nil {
				t.Fatal("failed recovery succeeded")
			}
			if failure == "save failure" {
				if !errors.Is(err, ErrSessionSave) {
					t.Fatal(err)
				}
				if err := os.Remove(paths[0]); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(paths[0], before[0], 0600); err != nil {
					t.Fatal(err)
				}
			}
			for i, path := range paths {
				after, e := os.ReadFile(path)
				if e != nil || !bytes.Equal(after, before[i]) {
					t.Fatalf("failure replaced saved state at %s", filepath.Base(path))
				}
			}
			if f.pinCalls.Load() != 0 {
				t.Fatal("recovery requested account linking")
			}
			if (failure == "account outage" || failure == "unrelated server" || failure == "identity mismatch" || failure == "HTTPS downgrade" || failure == "explicit configuration") && choices != 0 {
				t.Fatal("offered an invalid recovery")
			}
			if failure == "account outage" && f.connector.Describe(err).Title != "Plex server unavailable" {
				t.Fatal("outage did not explain preserved sign-in")
			}
		})
	}
}

func TestPlexRecoveryKeepsHTTPSBeforeRankingCandidates(t *testing.T) {
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("recovery probed an HTTP downgrade")
	}))
	defer local.Close()
	secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Plex-Token") != "" {
			t.Error("identity probe received credentials")
		}
		fmt.Fprint(w, `{"MediaContainer":{"machineIdentifier":"home"}}`)
	}))
	defer secure.Close()
	account := discoveryAccount(t, []accountResource{
		{ID: "other", Name: "Unrelated", Provides: "server", Token: "other-token", Connections: []resourceConnection{{URI: local.URL, Local: true}}},
		{ID: "home", Name: "Home", Provides: "server", Token: "server-token", Connections: []resourceConnection{{URI: local.URL, Local: true}, {URI: secure.URL}}},
	})
	account.HTTP = secure.Client()
	d := &serverDiscovery{account: account, previous: &connection.Server{ID: "home", Name: "Home", URL: "https://previous.invalid"}}
	choices, err := d.Discover(t.Context())
	if err != nil || len(choices) != 1 || choices[0].URL != secure.URL || len(d.grants) != 1 {
		t.Fatalf("choices=%v error=%v", choices, err)
	}
}
