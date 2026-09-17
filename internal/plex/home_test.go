package plex

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mistervision/internal/connection"
	"mistervision/internal/serverstate"
)

// TestHomeSelectionScopesCredentials covers the same Home flow for a discovered
// server and a configured address, including migration of the original sign-in.
func TestHomeSelectionScopesCredentials(t *testing.T) {
	for _, configured := range []bool{false, true} {
		t.Run(map[bool]string{false: "discovered", true: "configured"}[configured], func(t *testing.T) {
			f := newDiscoveryFixture(t, true)
			f.home.Store(true)
			if configured {
				f.connector.Config.Server = f.server.URL
				if err := serverstate.SaveSession(StateDir(f.connector.StateDir), serverstate.Session{Server: f.server.URL, Token: "account-token", UserID: "7", DeviceID: "device"}); err != nil {
					t.Fatal(err)
				}
			}
			picks := 0
			i := connection.Interaction{ChooseServer: chooseFirst, ChooseProfile: func(ctx context.Context, p connection.ProfilePrompt) (connection.ProfileSelection, error) {
				picks++
				if len(p.Profiles) != 3 || !p.Profiles[0].Protected || p.Profiles[1].Protected {
					t.Fatal("incorrect profile choices")
				}
				if f.mediaCalls.Load() != 0 {
					t.Fatal("libraries opened before profile selection")
				}
				return connection.ProfileSelection{ID: "8"}, nil
			}}
			session, err := f.connector.connectDiscovered(t.Context(), i, &serverDiscovery{account: f.account})
			if err != nil {
				t.Fatal(err)
			}
			client := session.Server.(*Client)
			if picks != 1 || !session.SwitchProfile || session.Profile.ID != "8" || client.Session.Token != "child-server-token" || client.Identity().User != "8" {
				t.Fatal("viewer did not scope the session")
			}
			state, err := loadDiscoveryState(StateDir(f.connector.StateDir))
			if err != nil || state.Account.UserID != "7" || state.Account.Token != "account-token" || state.Credentials.UserID != "8" || state.Profile.ID != "8" {
				t.Fatal("linking account was lost or mistaken for the viewer")
			}
			// An unprotected last viewer resumes without another choice.
			i.ChooseProfile = func(context.Context, connection.ProfilePrompt) (connection.ProfileSelection, error) {
				t.Fatal("unprotected viewer was not remembered")
				return connection.ProfileSelection{}, nil
			}
			reopened, err := f.connector.connectDiscovered(t.Context(), i, &serverDiscovery{account: f.account})
			if err != nil || reopened.Server.Identity() != client.Identity() {
				t.Fatalf("remembered viewer failed: %v", err)
			}
		})
	}
}

func TestProtectedHomeRequiresPINOnRestart(t *testing.T) {
	f := newDiscoveryFixture(t, true)
	f.home.Store(true)
	attempts := 0
	i := connection.Interaction{ChooseServer: chooseFirst, ChooseProfile: func(ctx context.Context, p connection.ProfilePrompt) (connection.ProfileSelection, error) {
		attempts++
		if f.mediaCalls.Load() != 0 {
			t.Fatal("protected media accessed before PIN validation")
		}
		if attempts == 1 {
			return connection.ProfileSelection{ID: "7", PIN: "9999"}, nil
		}
		if !p.PIN || p.Message == "" || p.Profiles[p.Selected].ID != "7" {
			t.Fatal("incorrect PIN did not return to its keypad")
		}
		return connection.ProfileSelection{ID: "7", PIN: "1234"}, nil
	}}
	if _, err := f.connector.connectDiscovered(t.Context(), i, &serverDiscovery{account: f.account}); err != nil {
		t.Fatal(err)
	}
	if attempts != 2 {
		t.Fatal("PIN retry missing")
	}
	path := filepath.Join(StateDir(f.connector.StateDir), "server.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(before, []byte("1234")) {
		t.Fatal("PIN persisted")
	}
	calls := f.mediaCalls.Load()
	i.ChooseProfile = func(ctx context.Context, p connection.ProfilePrompt) (connection.ProfileSelection, error) {
		if !p.PIN || p.Profiles[p.Selected].ID != "7" {
			t.Fatal("restart bypassed protected profile")
		}
		return connection.ProfileSelection{}, context.Canceled
	}
	if _, err := f.connector.connectDiscovered(t.Context(), i, &serverDiscovery{account: f.account}); !errors.Is(err, context.Canceled) {
		t.Fatal("protected restart was not cancellable")
	}
	if f.mediaCalls.Load() != calls {
		t.Fatal("saved protected grant bypassed the PIN")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("canceling changed the saved viewer")
	}
}

func TestHomeSwitchFailurePreservesCurrentViewer(t *testing.T) {
	for _, failure := range []string{"cancel", "forged profile", "no server access", "network failure"} {
		t.Run(failure, func(t *testing.T) {
			f := newDiscoveryFixture(t, true)
			f.home.Store(true)
			i := connection.Interaction{ChooseServer: chooseFirst, ChooseProfile: func(context.Context, connection.ProfilePrompt) (connection.ProfileSelection, error) {
				return connection.ProfileSelection{ID: "8"}, nil
			}}
			if _, err := f.connector.connectDiscovered(t.Context(), i, &serverDiscovery{account: f.account}); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(StateDir(f.connector.StateDir), "server.json")
			before, _ := os.ReadFile(path)
			i.SelectProfile = true
			i.ChooseProfile = func(context.Context, connection.ProfilePrompt) (connection.ProfileSelection, error) {
				switch failure {
				case "cancel":
					return connection.ProfileSelection{}, context.Canceled
				case "forged profile":
					return connection.ProfileSelection{ID: "not-offered"}, nil
				default:
					return connection.ProfileSelection{ID: "9"}, nil
				}
			}
			i.ChooseServer = func(ctx context.Context, servers []connection.Server) (connection.Server, error) {
				if len(servers) != 0 {
					t.Fatal("profile inherited another viewer's server access")
				}
				return connection.Server{}, context.Canceled
			}
			if failure == "network failure" {
				f.failAccount.Store(true)
			}
			if _, err := f.connector.connectDiscovered(t.Context(), i, &serverDiscovery{account: f.account}); err == nil {
				t.Fatal("failed switch succeeded")
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("failed switch replaced current viewer")
			}
		})
	}
}

func TestHomeServerBackReturnsToProfiles(t *testing.T) {
	f := newDiscoveryFixture(t, true)
	f.home.Store(true)
	picks, servers := 0, 0
	i := connection.Interaction{ChooseProfile: func(context.Context, connection.ProfilePrompt) (connection.ProfileSelection, error) {
		picks++
		return connection.ProfileSelection{ID: "8"}, nil
	}, ChooseServer: func(ctx context.Context, s []connection.Server) (connection.Server, error) {
		servers++
		if servers == 1 {
			return connection.Server{}, connection.ErrChooseProfile
		}
		return chooseFirst(ctx, s)
	}}
	if _, err := f.connector.connectDiscovered(t.Context(), i, &serverDiscovery{account: f.account}); err != nil {
		t.Fatal(err)
	}
	if picks != 2 || servers != 2 {
		t.Fatal("server Back did not return to profiles")
	}
}

func TestSingleHomeProfileSkipsPickerButHonorsPIN(t *testing.T) {
	for _, protected := range []bool{false, true} {
		t.Run(fmt.Sprint(protected), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/home/users" {
					fmt.Fprintf(w, `<MediaContainer><User id="7" title="Only viewer" protected="%t"/></MediaContainer>`, protected)
					return
				}
				if r.URL.Path != "/api/home/users/7/switch" {
					t.Error("unexpected endpoint")
				}
				fmt.Fprint(w, `<user id="7" authenticationToken="viewer-token"/>`)
			}))
			defer server.Close()
			client := NewClient(Config{}, serverstate.Session{UserID: "7", Token: "owner-token"})
			client.accountURL = server.URL
			d := serverDiscovery{account: client}
			prompts := 0
			err := d.chooseHome(t.Context(), connection.Interaction{ChooseProfile: func(ctx context.Context, p connection.ProfilePrompt) (connection.ProfileSelection, error) {
				prompts++
				if !protected || !p.PIN {
					t.Fatal("single-profile policy failed")
				}
				return connection.ProfileSelection{ID: "7", PIN: "1234"}, nil
			}}, accountIdentity{Home: true}, "")
			if err != nil {
				t.Fatal(err)
			}
			if (protected && prompts != 1) || (!protected && prompts != 0) {
				t.Fatal("unexpected picker")
			}
		})
	}
}

func TestHomeRejectsIncompleteProtectionMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<MediaContainer><User id="7" title="Parent"/></MediaContainer>`)
	}))
	defer server.Close()
	client := NewClient(Config{}, serverstate.Session{})
	client.accountURL = server.URL
	if _, _, err := client.homeProfiles(t.Context()); !errors.Is(err, errHome) {
		t.Fatal("missing protection metadata became an unprotected profile")
	}
}

func TestHomeAvatarsFollowOnlyPublicPlexRedirects(t *testing.T) {
	var imageData bytes.Buffer
	if err := png.Encode(&imageData, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	client := NewClient(Config{}, serverstate.Session{Token: "private-owner-token"})
	client.accountURL = "https://account.test"
	client.accountHTTP.Transport = discoveryTransportFunc(func(r *http.Request) (*http.Response, error) {
		response := &http.Response{StatusCode: 200, Header: make(http.Header)}
		body := ""
		switch r.URL.Host {
		case "account.test":
			body = `<MediaContainer><User id="1" title="Valid" protected="0" thumb="https://plex.tv/avatar?c=1"/><User id="2" title="Outside" protected="0" thumb="https://elsewhere.test/avatar"/><User id="3" title="Token query" protected="0" thumb="https://plex.tv/avatar?token=private"/><User id="4" title="Redirect outside" protected="0" thumb="https://plex.tv/outside"/></MediaContainer>`
		case "plex.tv":
			if r.Header.Get("X-Plex-Token") != "" {
				t.Error("avatar received account credentials")
			}
			response.StatusCode = 302
			location := "https://assets.plex.tv/avatar.png"
			if r.URL.Path == "/outside" {
				location = "https://elsewhere.test/avatar"
			}
			response.Header.Set("Location", location)
		case "assets.plex.tv":
			if r.Header.Get("X-Plex-Token") != "" {
				t.Error("asset redirect received credentials")
			}
			body = imageData.String()
		default:
			t.Error("untrusted avatar origin requested")
		}
		response.Body = io.NopCloser(strings.NewReader(body))
		return response, nil
	})
	profiles, avatars, err := client.homeProfiles(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := avatars.Load(t.Context(), profiles[0].ID)
	if err != nil || loaded == nil {
		t.Fatal("Plex asset redirect did not load")
	}
	for _, p := range profiles[1:] {
		if loaded, _ := avatars.Load(t.Context(), p.ID); loaded != nil {
			t.Fatal("unsafe avatar was loaded")
		}
	}
}

func TestProfileGrantRefreshKeepsWorkingAddress(t *testing.T) {
	f := newDiscoveryFixture(t, true)
	old := f.server
	replacement := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"MediaContainer":{"machineIdentifier":"server-id"}}`)
	}))
	defer replacement.Close()
	f.server.URL = replacement.URL
	f.account.Session = serverstate.Session{Token: "account-token", DeviceID: "device", UserID: "7"}
	d := serverDiscovery{account: f.account, preferred: &old}
	servers, err := d.Discover(t.Context())
	if err != nil || len(servers) != 1 || servers[0].URL != old.URL {
		t.Fatal("grant refresh changed a working address")
	}
}

func TestConfiguredConnectionRefreshesRejectedServerGrant(t *testing.T) {
	f := newDiscoveryFixture(t, true)
	f.connector.Config.Server = f.server.URL
	dir := StateDir(f.connector.StateDir)
	if err := serverstate.SaveSession(dir, serverstate.Session{Server: f.server.URL, Token: "account-token", UserID: "7", DeviceID: "device"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.connector.connectDiscovered(t.Context(), connection.Interaction{}, &serverDiscovery{account: f.account}); err != nil {
		t.Fatal(err)
	}
	state, err := loadDiscoveryState(dir)
	if err != nil {
		t.Fatal(err)
	}
	state.Credentials.Token = "expired"
	if err := saveDiscoveryState(dir, state); err != nil {
		t.Fatal(err)
	}
	result, err := f.connector.connectDiscovered(t.Context(), connection.Interaction{}, &serverDiscovery{account: f.account})
	if err != nil || result.Server.(*Client).Session.Token != "server-token" {
		t.Fatalf("configured grant did not refresh: %v", err)
	}
}

func TestHomeRemovedWhileServerPickerOpen(t *testing.T) {
	f := newDiscoveryFixture(t, true)
	f.home.Store(true)
	serverPicks := 0
	var presentation connection.Presentation
	i := connection.Interaction{
		Progress: func(p connection.Presentation) { presentation = p },
		ChooseProfile: func(context.Context, connection.ProfilePrompt) (connection.ProfileSelection, error) {
			return connection.ProfileSelection{ID: "8"}, nil
		},
		ChooseServer: func(ctx context.Context, servers []connection.Server) (connection.Server, error) {
			serverPicks++
			if serverPicks == 1 {
				f.home.Store(false)
				return connection.Server{}, connection.ErrChooseProfile
			}
			if presentation.BackToProfiles || !strings.Contains(presentation.Message, "Test Viewer") {
				t.Fatal("server picker retained the former Home profile")
			}
			return chooseFirst(ctx, servers)
		},
	}
	result, err := f.connector.connectDiscovered(t.Context(), i, &serverDiscovery{account: f.account})
	if err != nil || result.Profile != nil || result.SwitchProfile || result.Avatars != nil || serverPicks != 2 {
		t.Fatalf("membership change did not return to account server selection: %v", err)
	}
}

func TestHomeMetadataDoesNotDownloadAvatars(t *testing.T) {
	client := NewClient(Config{}, serverstate.Session{})
	client.accountURL = "https://account.test"
	client.accountHTTP.Transport = discoveryTransportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "account.test" {
			t.Fatal("membership blocked on an avatar request")
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`<MediaContainer><User id="1" title="Viewer" protected="0" thumb="https://plex.tv/avatar"/></MediaContainer>`))}, nil
	})
	profiles, source, err := client.homeProfiles(t.Context())
	if err != nil || len(profiles) != 1 || profiles[0].Avatar != nil || source == nil {
		t.Fatalf("metadata did not provide immediate profiles and deferred artwork: %v", err)
	}
}
