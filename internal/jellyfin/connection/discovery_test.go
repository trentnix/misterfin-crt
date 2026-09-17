package connection

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mistervision/internal/connection"
	"mistervision/internal/jellyfin"
	"mistervision/internal/serverstate"
)

type discoverFunc func(context.Context) ([]connection.Server, error)

func (f discoverFunc) Discover(ctx context.Context) ([]connection.Server, error) { return f(ctx) }

func TestDiscoverySelectionPersistsAndExplicitConfigurationWins(t *testing.T) {
	dir := t.TempDir()
	first := connection.Server{ID: "first", Name: "First", URL: "http://first:8096"}
	second := connection.Server{ID: "second", Name: "Second", URL: "http://second:8096"}
	scans, choices := 0, 0
	c := Connector{StateDir: dir, ConfigPath: filepath.Join(dir, "jellyfin.conf"), Discovery: discoverFunc(func(context.Context) ([]connection.Server, error) {
		scans++
		return []connection.Server{first, second}, nil
	})}
	interaction := connection.Interaction{ChooseServer: func(ctx context.Context, servers []connection.Server) (connection.Server, error) {
		choices++
		return servers[1], nil
	}}
	for range 2 {
		config, err := c.resolveConfig(t.Context(), interaction)
		if err != nil || config.Server != second.URL || config.InsecureTLS {
			t.Fatalf("selection: %#v %v", config, err)
		}
	}
	if scans != 1 || choices != 1 {
		t.Fatalf("repeated setup: scans %d choices %d", scans, choices)
	}
	interaction.SelectServer = true // Explicit configuration still wins when going back.
	if err := os.WriteFile(c.ConfigPath, []byte("https://configured:8920\nINSECURE_TLS\n"), 0600); err != nil {
		t.Fatal(err)
	}
	config, err := c.resolveConfig(t.Context(), interaction)
	if err != nil || config.Server != "https://configured:8920" || !config.InsecureTLS || scans != 1 {
		t.Fatal("legacy config did not win")
	}
	if err := os.WriteFile(c.ConfigPath, []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := c.resolveConfig(t.Context(), interaction); err == nil || scans != 1 {
		t.Fatal("invalid explicit config triggered discovery")
	}
	c.Config = &jellyfin.Config{Server: "https://json:8920"}
	config, err = c.resolveConfig(t.Context(), interaction)
	if err != nil || config.Server != c.Config.Server || scans != 1 {
		t.Fatal("JSON config did not win")
	}
	saved, err := serverstate.LoadServer(filepath.Join(dir, "jellyfin-server.json"))
	if err != nil || saved != second {
		t.Fatal("explicit config changed remembered selection")
	}
}

func TestDiscoveryFailuresAndCancellationDoNotSaveSelection(t *testing.T) {
	candidate := connection.Server{ID: "one", Name: "One", URL: "http://one:8096"}
	for _, kind := range []string{"empty", "network", "cancel", "unoffered", "storage", "damaged"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			scanned := false
			c := Connector{StateDir: dir, ConfigPath: filepath.Join(dir, "jellyfin.conf"), SettingsPath: filepath.Join(dir, "settings.json"), Discovery: discoverFunc(func(context.Context) ([]connection.Server, error) {
				scanned = true
				if kind == "empty" {
					return nil, nil
				}
				if kind == "network" {
					return nil, errors.New("private network detail")
				}
				return []connection.Server{candidate}, nil
			})}
			path := filepath.Join(dir, "jellyfin-server.json")
			if kind == "damaged" {
				if err := os.WriteFile(path, []byte("broken"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			_, err := c.resolveConfig(ctx, connection.Interaction{ChooseServer: func(context.Context, []connection.Server) (connection.Server, error) {
				if kind == "cancel" {
					cancel()
				}
				if kind == "unoffered" {
					return connection.Server{}, nil
				}
				if kind == "storage" {
					if err := os.Mkdir(path, 0700); err != nil {
						t.Fatal(err)
					}
				}
				return candidate, nil
			}})
			if err == nil {
				t.Fatal("failure succeeded")
			}
			if kind == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			if kind == "damaged" && scanned {
				t.Fatal("damaged saved choice silently switched servers")
			}
			if kind == "empty" && c.Describe(err).Title != "No Jellyfin servers found" {
				t.Fatal("missing actionable empty result")
			}
			if kind == "network" && c.Describe(err).Message == err.Error() {
				t.Fatal("raw network error displayed")
			}
			if kind != "damaged" && kind != "storage" {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatal("failed selection was saved")
				}
			}
		})
	}
}

func TestDiscoveredServerQuickConnectAndSavedSignIn(t *testing.T) {
	approvals := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/QuickConnect/Enabled":
			fmt.Fprint(w, "true")
		case "/QuickConnect/Initiate":
			fmt.Fprint(w, `{"Code":"123456","Secret":"private-secret"}`)
		case "/QuickConnect/Connect":
			fmt.Fprint(w, `{"Authenticated":true}`)
		case "/Users/AuthenticateWithQuickConnect":
			fmt.Fprint(w, `{"AccessToken":"saved-token","User":{"Id":"viewer"}}`)
		case "/UserViews":
			fmt.Fprint(w, `{"Items":[]}`)
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	candidate := connection.Server{ID: "server-id", Name: "Test server", URL: server.URL}
	scans := 0
	dir := t.TempDir()
	c := Connector{StateDir: dir, ConfigPath: filepath.Join(dir, "jellyfin.conf"), Discovery: discoverFunc(func(context.Context) ([]connection.Server, error) {
		scans++
		return []connection.Server{candidate}, nil
	})}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	interaction := connection.Interaction{
		ChooseServer: func(context.Context, []connection.Server) (connection.Server, error) { return candidate, nil },
		Progress: func(p connection.Presentation) {
			if p.Kind == connection.SetupApproval {
				approvals++
				if p.Code != "123456" || strings.Contains(fmt.Sprint(p), "private-secret") {
					t.Error("unsafe approval presentation")
				}
			}
		},
	}
	for range 2 {
		session, err := c.Connect(ctx, interaction)
		if err != nil || session.Server == nil || session.Remote == nil {
			t.Fatalf("discovered connection: %v", err)
		}
	}
	if scans != 1 || approvals != 1 {
		t.Fatalf("repeated setup: scans %d, approvals %d", scans, approvals)
	}
	if _, err := os.Stat(c.ConfigPath); !os.IsNotExist(err) {
		t.Fatal("discovery wrote legacy configuration")
	}
}

// TestReselectServerPreservesSavedChoiceUntilSelection checks that returning to
// discovery bypasses a saved choice without deleting it on failure or cancel.
func TestReselectServerPreservesSavedChoiceUntilSelection(t *testing.T) {
	first := connection.Server{ID: "first", Name: "First", URL: "http://first:8096"}
	second := connection.Server{ID: "second", Name: "Second", URL: "http://second:8096"}
	for _, outcome := range []string{"select", "cancel", "empty", "network"} {
		t.Run(outcome, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "jellyfin-server.json")
			if err := serverstate.SaveServer(path, first); err != nil {
				t.Fatal(err)
			}
			scans := 0
			c := Connector{StateDir: dir, ConfigPath: filepath.Join(dir, "jellyfin.conf"), Discovery: discoverFunc(func(context.Context) ([]connection.Server, error) {
				scans++
				if outcome == "empty" {
					return nil, nil
				}
				if outcome == "network" {
					return nil, errors.New("offline")
				}
				return []connection.Server{first, second}, nil
			})}
			config, err := c.resolveConfig(t.Context(), connection.Interaction{SelectServer: true, ChooseServer: func(context.Context, []connection.Server) (connection.Server, error) {
				if outcome == "cancel" {
					return connection.Server{}, context.Canceled
				}
				return second, nil
			}})
			expected := first
			if outcome == "select" {
				expected = second
				if err != nil || config.Server != second.URL {
					t.Fatalf("reselection: %v", err)
				}
			} else if err == nil {
				t.Fatal("unsuccessful discovery returned a server")
			}
			saved, loadErr := serverstate.LoadServer(path)
			if loadErr != nil || saved != expected || scans != 1 {
				t.Fatalf("saved choice: %#v, error: %v, scans: %d", saved, loadErr, scans)
			}
			// New-code retries and later launches use the choice without rescanning.
			config, err = c.resolveConfig(t.Context(), connection.Interaction{})
			if err != nil || config.Server != expected.URL || scans != 1 {
				t.Fatal("normal retry did not reuse saved selection")
			}
		})
	}
}
