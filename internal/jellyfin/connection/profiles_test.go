package connection

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
	"testing"

	"mistervision/internal/connection"
	"mistervision/internal/jellyfin"
)

// userFixture rejects mismatched tokens and serves distinct libraries per user.
type userFixture struct {
	connector *Connector
	users     userStore
	reject    string
	approved  string
}

func newUserFixture(t *testing.T) *userFixture {
	t.Helper()
	f := &userFixture{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := ""
		for _, u := range f.users {
			if strings.Contains(r.Header.Get("Authorization"), `Token="`+u.Session.Token+`"`) {
				id = u.User.ID
			}
		}
		switch r.URL.Path {
		case "/UserViews":
			if id == "" || id == f.reject {
				w.WriteHeader(401)
				return
			}
			if r.URL.Query().Get("userId") != id {
				t.Error("library request used the wrong user")
			}
			fmt.Fprint(w, `{"Items":[]}`)
		case "/Users/Me":
			if id == "" || id == f.reject {
				w.WriteHeader(401)
				return
			}
			for _, user := range f.users {
				if user.User.ID == id {
					json.NewEncoder(w).Encode(user.User)
					return
				}
			}
		case "/QuickConnect/Enabled":
			fmt.Fprint(w, "true")
		case "/QuickConnect/Initiate":
			if id != "" {
				t.Error("Quick Connect reused a saved token")
			}
			fmt.Fprint(w, `{"Code":"123456","Secret":"private-secret"}`)
		case "/QuickConnect/Connect":
			fmt.Fprint(w, `{"Authenticated":true}`)
		case "/Users/AuthenticateWithQuickConnect":
			id = f.approved
			fmt.Fprintf(w, `{"AccessToken":%q,"User":{"Id":%q,"Name":%q}}`, "token-"+id, id, "Viewer "+id)
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(server.Close)
	dir := t.TempDir()
	for _, id := range []string{"one", "two"} {
		f.users = append(f.users, savedUser{User: jellyfin.User{ID: id, Name: "Viewer " + id}, Session: jellyfin.Session{Server: server.URL, UserID: id, Token: "token-" + id, DeviceID: "device-" + id}})
	}
	if err := f.users.save(dir); err != nil {
		t.Fatal(err)
	}
	if err := jellyfin.SaveSession(dir, f.users[0].Session); err != nil {
		t.Fatal(err)
	}
	f.connector = &Connector{Config: &jellyfin.Config{Server: server.URL}, StateDir: dir}
	return f
}

func TestSavedJellyfinUserSwitchAndRestart(t *testing.T) {
	f := newUserFixture(t)
	result, err := f.connector.Connect(t.Context(), connection.Interaction{ProfileAction: connection.ProfileChoose, ChooseProfile: func(ctx context.Context, p connection.ProfilePrompt) (connection.ProfileSelection, error) {
		if len(p.Profiles) != 2 || p.Selected != 0 || !p.AddUser || p.PIN {
			t.Fatal("wrong Jellyfin profile prompt")
		}
		return connection.ProfileSelection{ID: "two"}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if result.ProfileAction != connection.ProfileChoose || result.Profile == nil || result.Profile.Name != "Viewer two" || result.Server.Identity().User != "two" {
		t.Fatal("wrong authenticated profile")
	}
	restarted := &Connector{Config: f.connector.Config, StateDir: f.connector.StateDir}
	result, err = restarted.Connect(t.Context(), connection.Interaction{})
	if err != nil || result.Server.Identity().User != "two" {
		t.Fatalf("last user not restored: %v", err)
	}
	users, err := loadUsers(f.connector.StateDir)
	if err != nil || len(users) != 2 || users[0].Session.Token != "token-one" {
		t.Fatal("switch lost the other sign-in")
	}
	info, err := os.Stat(filepath.Join(f.connector.StateDir, "jellyfin-users.json"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("saved credentials are not private")
	}
}

func TestFailedJellyfinUserSwitchKeepsActiveSession(t *testing.T) {
	for _, scenario := range []string{"cancel-picker", "unoffered", "cancel-code", "revoked", "storage"} {
		t.Run(scenario, func(t *testing.T) {
			f := newUserFixture(t)
			path := filepath.Join(f.connector.StateDir, "session.json")
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			interaction := connection.Interaction{ProfileAction: connection.ProfileChoose, ChooseProfile: func(ctx context.Context, p connection.ProfilePrompt) (connection.ProfileSelection, error) {
				switch scenario {
				case "cancel-picker":
					return connection.ProfileSelection{}, context.Canceled
				case "unoffered":
					return connection.ProfileSelection{ID: "admin"}, nil
				case "cancel-code":
					return connection.ProfileSelection{Action: connection.ProfileAdd}, nil
				case "revoked":
					f.reject = "two"
				case "storage":
					// Saving the active user fails after the selected token validates.
					if err := os.Rename(path, path+".previous"); err != nil {
						t.Fatal(err)
					}
					if err := os.Mkdir(path, 0700); err != nil {
						t.Fatal(err)
					}
				}
				return connection.ProfileSelection{ID: "two"}, nil
			}, Progress: func(p connection.Presentation) {
				if p.Kind == connection.SetupApproval {
					if p.Back != connection.BackProfiles {
						t.Error("code lost return-to-profiles action")
					}
					cancel()
				}
			}}
			result, err := f.connector.Connect(ctx, interaction)
			if err == nil || result.Server != nil {
				t.Fatal("failed switch returned a session")
			}
			if scenario == "storage" {
				path += ".previous"
			}
			after, err := os.ReadFile(path)
			if err != nil || string(after) != string(before) {
				t.Fatal("failed switch replaced active sign-in")
			}
		})
	}
}

func TestJellyfinAddUserUsesIndependentAuthorization(t *testing.T) {
	f := newUserFixture(t)
	// Add a server identity without preauthorizing it in the saved roster.
	third := savedUser{User: jellyfin.User{ID: "three", Name: "Viewer three"}, Session: jellyfin.Session{Server: f.connector.Config.Server, Token: "token-three", UserID: "three", DeviceID: "unused"}}
	f.users = append(f.users, third)
	f.approved = "three"
	result, err := f.connector.Connect(t.Context(), connection.Interaction{ProfileAction: connection.ProfileChoose, ChooseProfile: func(context.Context, connection.ProfilePrompt) (connection.ProfileSelection, error) {
		return connection.ProfileSelection{Action: connection.ProfileAdd}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	session := result.Server.(*jellyfin.Client).Session
	if session.UserID != "three" || session.DeviceID == "device-one" || session.DeviceID == "device-two" || session.DeviceID == "" {
		t.Fatal("new user reused another identity")
	}
	saved, err := loadUsers(f.connector.StateDir)
	if err != nil || len(saved) != 3 {
		t.Fatal("new user was not saved")
	}
}

func TestJellyfinUsersStayBoundToServer(t *testing.T) {
	f := newUserFixture(t)
	other := savedUser{User: jellyfin.User{ID: "foreign", Name: "Other server"}, Session: jellyfin.Session{Server: "http://elsewhere", UserID: "foreign", Token: "secret", DeviceID: "other"}}
	f.users = append(f.users, other)
	if err := f.users.save(f.connector.StateDir); err != nil {
		t.Fatal(err)
	}
	_, err := f.connector.Connect(t.Context(), connection.Interaction{ProfileAction: connection.ProfileChoose, ChooseProfile: func(ctx context.Context, p connection.ProfilePrompt) (connection.ProfileSelection, error) {
		if len(p.Profiles) != 2 {
			t.Error("offered a user from another server")
		}
		if _, err := p.Avatars.Load(ctx, "foreign"); !errors.Is(err, errProfileSelection) {
			t.Error("loaded an unoffered avatar")
		}
		return connection.ProfileSelection{}, context.Canceled
	}})
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestExistingQuickConnectUserIsAddedToPicker(t *testing.T) {
	f := newUserFixture(t)
	if err := os.Remove(filepath.Join(f.connector.StateDir, "jellyfin-users.json")); err != nil {
		t.Fatal(err)
	}
	result, err := f.connector.Connect(t.Context(), connection.Interaction{})
	if err != nil || result.ProfileAction != connection.ProfileAdd {
		t.Fatalf("existing sign-in did not migrate: %v", err)
	}
	users, err := loadUsers(f.connector.StateDir)
	if err != nil || len(users) != 1 || users[0].Session != f.users[0].Session {
		t.Fatal("migration changed the original token or device")
	}
}

func TestJellyfinUserMetadataFailureDoesNotCommitSwitch(t *testing.T) {
	for _, mode := range []string{"wrong-identity", "unavailable"} {
		t.Run(mode, func(t *testing.T) {
			f := newUserFixture(t)
			// Keep both saved credentials valid for library requests, but make the
			// metadata endpoint fail or return another user's public identity.
			original := f.connector.Config.Server
			proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/Users/Me" {
					if mode == "unavailable" {
						w.WriteHeader(503)
						return
					}
					fmt.Fprint(w, `{"Id":"one","Name":"Viewer one"}`)
					return
				}
				fmt.Fprint(w, `{"Items":[]}`)
			}))
			defer proxy.Close()
			f.connector.Config.Server = proxy.URL
			for i := range f.users {
				f.users[i].Session.Server = proxy.URL
			}
			if err := f.users.save(f.connector.StateDir); err != nil {
				t.Fatal(err)
			}
			if err := jellyfin.SaveSession(f.connector.StateDir, f.users[0].Session); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(filepath.Join(f.connector.StateDir, "session.json"))
			if err != nil {
				t.Fatal(err)
			}
			result, err := f.connector.Connect(t.Context(), connection.Interaction{ProfileAction: connection.ProfileChoose, ChooseProfile: func(context.Context, connection.ProfilePrompt) (connection.ProfileSelection, error) {
				return connection.ProfileSelection{ID: "two"}, nil
			}})
			if err == nil || result.Server != nil {
				t.Fatal("invalid viewer metadata committed a switch")
			}
			after, err := os.ReadFile(filepath.Join(f.connector.StateDir, "session.json"))
			if err != nil || string(after) != string(before) {
				t.Fatal("failed validation replaced the active user")
			}
			f.connector.Config.Server = original
		})
	}
}

func TestOnlySavedUserOffersDirectAddAndKeepsBackRoute(t *testing.T) {
	f := newUserFixture(t)
	roster := append(userStore(nil), f.users[:1]...)
	// A saved user on another server must not enable this server's picker.
	foreign := f.users[1]
	foreign.Session.Server = "http://other-server"
	roster = append(roster, foreign)
	if err := roster.save(f.connector.StateDir); err != nil {
		t.Fatal(err)
	}
	current, err := f.connector.Connect(t.Context(), connection.Interaction{})
	if err != nil || current.ProfileAction != connection.ProfileAdd {
		t.Fatalf("single user offered a switch: %v", err)
	}
	before, err := os.ReadFile(filepath.Join(f.connector.StateDir, "session.json"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	interaction := connection.Interaction{ProfileAction: connection.ProfileAdd, ChooseProfile: func(context.Context, connection.ProfilePrompt) (connection.ProfileSelection, error) {
		t.Fatal("single user opened a picker")
		return connection.ProfileSelection{}, nil
	}, Progress: func(p connection.Presentation) {
		if p.Kind == connection.SetupApproval {
			if p.Back != connection.BackConnection || p.Retry != "New code" {
				t.Error("direct sign-in has the wrong navigation")
			}
			cancel()
		}
	}}
	if _, err := f.connector.Connect(ctx, interaction); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel direct sign-in: %v", err)
	}
	after, err := os.ReadFile(filepath.Join(f.connector.StateDir, "session.json"))
	if err != nil || string(after) != string(before) {
		t.Fatal("canceling replaced the active user")
	}
	interaction.Progress = nil
	f.approved = "two"
	added, err := f.connector.Connect(t.Context(), interaction)
	if err != nil || added.ProfileAction != connection.ProfileChoose || added.Profile.ID != "two" {
		t.Fatalf("adding another user did not enable switching: %v", err)
	}
}

func TestJellyfinReconnectRefreshesNameAndAvatarReference(t *testing.T) {
	f := newUserFixture(t)
	for _, name := range []string{"First name", "Updated name"} {
		f.users[0].User.Name = name
		f.users[0].User.PrimaryImageTag = name + "-avatar"
		result, err := f.connector.Connect(t.Context(), connection.Interaction{})
		if err != nil || result.Profile.Name != name || result.Profile.AvatarKey != name+"-avatar" {
			t.Fatalf("stale public identity: %v", err)
		}
		users, err := loadUsers(f.connector.StateDir)
		if err != nil || users[0].User.Name != name || users[0].User.PrimaryImageTag != name+"-avatar" {
			t.Fatal("picker metadata was not refreshed")
		}
	}
}
