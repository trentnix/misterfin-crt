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
	newAccount                         atomic.Bool
	noServers                          atomic.Bool
	home                               atomic.Bool
	switchCalls                        atomic.Int32
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
			if r.Header.Get("X-Plex-Token") == "expired" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			if token := r.Header.Get("X-Plex-Token"); token != "server-token" && token != "new-server-token" && token != "child-server-token" {
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
			token := "account-token"
			if f.newAccount.Load() {
				token = "new-account-token"
			}
			fmt.Fprintf(w, `{"id":2,"code":"ABCD","expiresIn":30,"authToken":%q}`, token)
		case "/api/v2/user":
			if f.rejectAccount.Load() && r.Header.Get("X-Plex-Token") == "expired" {
				w.WriteHeader(401)
				return
			}
			if r.Header.Get("X-Plex-Token") == "new-account-token" {
				fmt.Fprint(w, `{"id":8,"friendlyName":"Another Viewer"}`)
				return
			}
			if r.Header.Get("X-Plex-Token") != "account-token" {
				t.Error("wrong account credential")
			}
			fmt.Fprintf(w, `{"id":7,"username":"tester","friendlyName":"Test Viewer","home":%t,"protected":%t}`, f.home.Load(), f.home.Load())
		case "/api/home/users":
			fmt.Fprint(w, `<MediaContainer><User id="7" title="Parent" protected="1"/><User id="8" title="Child" protected="0"/><User id="9" title="Guest" protected="0"/></MediaContainer>`)
		case "/api/home/users/7/switch", "/api/home/users/8/switch", "/api/home/users/9/switch":
			f.switchCalls.Add(1)
			if r.Method != "POST" || r.URL.RawQuery != "" || r.Header.Get("X-Plex-Token") != "account-token" {
				t.Error("unsafe Home switch request")
			}
			if err := r.ParseForm(); err != nil {
				t.Error(err)
			}
			id := strings.Split(r.URL.Path, "/")[4]
			if id == "7" && r.PostForm.Get("pin") != "1234" {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			token := "account-token"
			if id == "8" {
				token = "child-account-token"
			}
			if id == "9" {
				token = "guest-account-token"
			}
			fmt.Fprintf(w, `<user id="%s" authenticationToken="%s"/>`, id, token)
		case "/api/v2/resources":
			token := "server-token"
			if r.Header.Get("X-Plex-Token") == "new-account-token" {
				token = "new-server-token"
			} else if r.Header.Get("X-Plex-Token") == "child-account-token" {
				token = "child-server-token"
			} else if r.Header.Get("X-Plex-Token") == "guest-account-token" {
				fmt.Fprint(w, `[]`)
				return
			} else if r.Header.Get("X-Plex-Token") != "account-token" {
				t.Error("wrong resource credential")
			}
			if f.noServers.Load() {
				fmt.Fprint(w, `[]`)
				return
			}
			json.NewEncoder(w).Encode([]accountResource{{Name: f.server.Name, ID: f.server.ID, Provides: "server", Token: token, Connections: []resourceConnection{{URI: f.server.URL, Local: true}}}})
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
	if result.Endpoint != f.server {
		t.Fatal("selected server metadata missing from session")
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
	if err != nil || reopened.Server.Identity() != result.Server.Identity() || reopened.Endpoint != result.Endpoint || f.accountCalls.Load() != accountCalls {
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
			paths := []string{filepath.Join(dir, "server.json")}
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
	before, err := loadDiscoveryState(dir)
	if err != nil {
		t.Fatal(err)
	}
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/identity" {
			fmt.Fprint(w, `{"MediaContainer":{"machineIdentifier":"server-id"}}`)
			return
		}
		fmt.Fprint(w, `{"MediaContainer":{"Directory":[]}}`)
	}))
	defer other.Close()
	f.server.URL = other.URL
	f.newAccount.Store(true)
	_, err = f.connector.connectDiscovered(t.Context(), connection.Interaction{NewAccount: true, ChooseServer: func(ctx context.Context, servers []connection.Server) (connection.Server, error) {
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
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, selection, 0600); err != nil {
		t.Fatal(err)
	}
	after, err := loadDiscoveryState(dir)
	if err != nil || *before.Account != *after.Account || *before.Credentials != *after.Credentials {
		t.Fatal("failed selection changed saved credentials")
	}
	if _, err := f.connector.connectDiscovered(t.Context(), connection.Interaction{}, &serverDiscovery{account: f.account}); err != nil {
		t.Fatalf("previous connection could not reopen: %v", err)
	}
}

// TestPlexAccountReplacement exercises a different user on the same server.
// Approval and rescanning must not commit that identity before selection succeeds.
func TestPlexAccountReplacement(t *testing.T) {
	for _, outcome := range []string{"success", "cancel code", "cancel selection", "access denied"} {
		t.Run(outcome, func(t *testing.T) {
			f := newDiscoveryFixture(t, true)
			old, err := f.connector.connectDiscovered(t.Context(), connection.Interaction{ChooseServer: chooseFirst}, &serverDiscovery{account: f.account})
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(StateDir(f.connector.StateDir), "server.json")
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			f.newAccount.Store(true)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			choices, codes := 0, 0
			result, err := f.connector.connectDiscovered(ctx, connection.Interaction{
				NewAccount: true,
				Progress: func(p connection.Presentation) {
					if p.Kind == connection.SetupApproval {
						codes++
						if !p.BackToServers {
							t.Error("new sign-in cannot return to saved picker")
						}
						if outcome == "cancel code" {
							cancel()
						}
					}
					if p.Kind == connection.SetupServers && (!strings.Contains(p.Message, "Another Viewer") || p.SignIn == "") {
						t.Error("picker lost replacement account or sign-in action")
					}
				},
				ChooseServer: func(ctx context.Context, servers []connection.Server) (connection.Server, error) {
					choices++
					after, e := os.ReadFile(path)
					if e != nil || string(after) != string(before) {
						t.Fatal("approval replaced saved account before selection")
					}
					if choices == 1 {
						return connection.Server{}, connection.ErrRescan
					}
					if outcome == "cancel selection" {
						return connection.Server{}, context.Canceled
					}
					if outcome == "access denied" {
						f.denyMedia.Store(true)
					}
					return chooseFirst(ctx, servers)
				},
			}, &serverDiscovery{account: f.account})
			if codes != 1 || f.pinCalls.Load() != 1 {
				t.Fatal("rescan relinked the account")
			}
			if outcome == "success" {
				if err != nil {
					t.Fatal(err)
				}
				if result.Endpoint != f.server {
					t.Fatal("selected server metadata missing from session")
				}
				client := result.Server.(*Client)
				if client.Identity() == old.Server.Identity() || client.Session.UserID != "8" || client.Session.Token != "new-server-token" {
					t.Fatal("replacement did not change playback identity and grant")
				}
				state, e := loadDiscoveryState(filepath.Dir(path))
				if e != nil || state.Account.Token != "new-account-token" || state.Credentials.Token != "new-server-token" {
					t.Fatal("account and grant were not committed together")
				}
			} else {
				if err == nil {
					t.Fatal("canceled or failed replacement succeeded")
				}
				after, e := os.ReadFile(path)
				if e != nil || string(after) != string(before) {
					t.Fatal("replacement damaged working state")
				}
			}
			f.denyMedia.Store(false)
			f.failAccount.Store(true)
			reopened, e := f.connector.Connect(t.Context(), connection.Interaction{})
			if e != nil {
				t.Fatal(e)
			}
			want := old.Server.Identity()
			if outcome == "success" {
				want = result.Server.Identity()
			}
			if reopened.Server.Identity() != want {
				t.Fatal("reopen used the wrong account")
			}
		})
	}
}

func TestPlexEmptyAccountCanRescanOrSignIn(t *testing.T) {
	f := newDiscoveryFixture(t, true)
	f.noServers.Store(true)
	choices := 0
	var presentation connection.Presentation
	_, err := f.connector.connectDiscovered(t.Context(), connection.Interaction{
		Progress: func(p connection.Presentation) {
			if p.Kind == connection.SetupServers {
				presentation = p
			}
		},
		ChooseServer: func(ctx context.Context, servers []connection.Server) (connection.Server, error) {
			choices++
			if presentation.SignIn == "" {
				t.Fatal("empty account cannot sign in again")
			}
			if choices == 1 {
				if len(servers) != 0 || !strings.Contains(presentation.Message, "No reachable servers") {
					t.Fatal("empty account not explained")
				}
				f.noServers.Store(false)
				return connection.Server{}, connection.ErrRescan
			}
			if len(servers) != 1 || strings.Contains(presentation.Message, "No reachable servers") {
				t.Fatal("rescan retained stale empty message")
			}
			return chooseFirst(ctx, servers)
		},
	}, &serverDiscovery{account: f.account})
	if err != nil || choices != 2 || f.pinCalls.Load() != 0 {
		t.Fatalf("rescan failed: %v", err)
	}
}

func TestConfiguredServerAddressCanChange(t *testing.T) {
	f := newDiscoveryFixture(t, true)
	f.connector.Config.Server = f.server.URL
	_, err := f.connector.connectDiscovered(t.Context(), connection.Interaction{}, &serverDiscovery{account: f.account})
	if err != nil {
		t.Fatal(err)
	}
	// localhost reaches the same httptest listener and /identity server.
	original := f.server.URL
	f.server.URL = "http://localhost" + original[len("http://127.0.0.1"):]
	f.connector.Config.Server = f.server.URL
	_, err = f.connector.connectDiscovered(t.Context(), connection.Interaction{}, &serverDiscovery{account: f.account})
	if err != nil {
		t.Fatalf("valid configured endpoint change failed: %v", err)
	}
}

// A changed configuration must obtain a matching grant before sending any
// media credentials. Failure leaves the previous working connection on disk.
func TestConfiguredAddressWithoutGrantPreservesConnection(t *testing.T) {
	f := newDiscoveryFixture(t, true)
	f.connector.Config.Server = f.server.URL
	if _, err := f.connector.connectDiscovered(t.Context(), connection.Interaction{}, &serverDiscovery{account: f.account}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(StateDir(f.connector.StateDir), "server.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/identity" || r.Header.Get("X-Plex-Token") != "" {
			t.Error("unverified endpoint received credentials or media requests")
		}
		fmt.Fprint(w, `{"MediaContainer":{"machineIdentifier":"another-server"}}`)
	}))
	defer other.Close()
	f.connector.Config.Server = other.URL
	if _, err := f.connector.connectDiscovered(t.Context(), connection.Interaction{}, &serverDiscovery{account: f.account}); err == nil {
		t.Fatal("ungranted endpoint connected")
	}
	after, err := os.ReadFile(path)
	if err != nil || string(before) != string(after) {
		t.Fatal("failed endpoint change replaced saved connection")
	}
}
