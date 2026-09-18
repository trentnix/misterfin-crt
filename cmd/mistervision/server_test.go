package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mistervision/internal/connection"
	jfconnection "mistervision/internal/jellyfin/connection"
	"mistervision/internal/plex"
	"mistervision/internal/settings"
)

func TestServerSelection(t *testing.T) {
	for _, tc := range []struct {
		name, body, provider string
		fail                 bool
	}{
		{"default", `{}`, "jellyfin", false},
		{"explicit Jellyfin", `{"server":{"provider":"jellyfin","url":"http://server:8096"}}`, "jellyfin", false},
		{"Plex", `{"server":{"provider":"plex","url":"http://server:32400"}}`, "plex", false},
		{"unknown", `{"server":{"provider":"other"}}`, "", true},
		{"null", `{"server":null}`, "", true},
		{"default provider", `{"server":{"url":"http://server"}}`, "jellyfin", false},
		{"unknown key", `{"server":{"provider":"plex","token":"private"}}`, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "settings.json")
			if err := os.WriteFile(path, []byte(tc.body), 0600); err != nil {
				t.Fatal(err)
			}
			source, err := settings.Load(path, true)
			if err != nil {
				t.Fatal(err)
			}
			connector, err := serverConnector(source, "jellyfin.conf", dir, "test", nil)
			if (err != nil) != tc.fail {
				t.Fatalf("selection failure = %v, want %v", err, tc.fail)
			}
			if tc.fail {
				return
			}
			switch c := connector.(type) {
			case *jfconnection.Connector:
				if (c.Discovery != nil) != (tc.name == "default") {
					t.Fatal("discovery must be available only without explicit server settings")
				}
				if tc.provider != "jellyfin" || c.StateDir != dir {
					t.Fatal("wrong Jellyfin selection")
				}
			case plex.Connector:
				if tc.provider != "plex" || c.StateDir != dir || c.Config.Server != "http://server:32400" {
					t.Fatal("wrong Plex selection")
				}
			default:
				t.Fatalf("unexpected connector %T", connector)
			}
		})
	}
}

func TestUnifiedServerOverridesLegacyConnectionAndDiagnostics(t *testing.T) {
	for _, provider := range []string{"jellyfin", "plex"} {
		t.Run(provider, func(t *testing.T) {
			dir := t.TempDir()
			legacy := filepath.Join(dir, "jellyfin.conf")
			path := filepath.Join(dir, "settings.json")
			if err := os.WriteFile(legacy, []byte("http://wrong-server\nwrong-token\nwrong-user\nDEBUGLOG\nINSECURE_TLS\n"), 0600); err != nil {
				t.Fatal(err)
			}
			body := `{"server":{"provider":"` + provider + `","url":"http://selected-server","transcode":{"max_width":640,"max_height":480,"video_bitrate":8000000}}}`
			if err := os.WriteFile(path, []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			source, err := settings.Load(path, true)
			if err != nil {
				t.Fatal(err)
			}
			connector, err := serverConnector(source, legacy, dir, "test", nil)
			if err != nil {
				t.Fatal(err)
			}
			switch c := connector.(type) {
			case *jfconnection.Connector:
				if c.Config == nil || c.Config.Server != "http://selected-server" || c.Config.APIKey != "" || c.Config.InsecureTLS || c.Config.Transcode.MaxWidth != 640 || c.Config.Transcode.MaxHeight != 480 || c.Config.Transcode.VideoBitrate != 8000000 || c.ConfigPath != path {
					t.Fatal("JSON did not control Jellyfin connection")
				}
			case plex.Connector:
				if c.Config.Server != "http://selected-server" || c.Config.InsecureTLS || c.Config.MaxWidth != 640 || c.Config.MaxHeight != 480 || c.Config.VideoBitrate != 8000000 {
					t.Fatal("JSON did not control Plex connection")
				}
			}
			trace, err := openStartupDiagnostics(launchOptions{browse: true, config: legacy}, false, source)
			if err != nil {
				t.Fatal(err)
			}
			trace.close(nil)
			if _, err := os.Stat(filepath.Join(dir, "debug.log")); !os.IsNotExist(err) {
				t.Fatal("legacy DEBUGLOG affected explicit server")
			}
		})
	}
}

func TestConnectionMigrationPreservesCredentialsAndLegacyFile(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(fmt.Sprint(existing), func(t *testing.T) {
			dir := t.TempDir()
			legacy := filepath.Join(dir, "jellyfin.conf")
			path := filepath.Join(dir, "settings.json")
			original := "https://server/jellyfin\nprivate-key\nviewer\nNTSC\nINSECURE_TLS\nDEBUGLOG\n640x480@8000000\n"
			if err := os.WriteFile(legacy, []byte(original), 0600); err != nil {
				t.Fatal(err)
			}
			if existing {
				if err := os.WriteFile(path, []byte(`{"display":{"interlaced":true}}`), 0600); err != nil {
					t.Fatal(err)
				}
			}
			source, err := settings.Load(path, false)
			if err != nil {
				t.Fatal(err)
			}
			if err := migrateSettings(launchOptions{config: legacy}, source); err != nil {
				t.Fatal(err)
			}
			next, err := settings.Load(path, true)
			if err != nil {
				t.Fatal(err)
			}
			server, err := settings.ParseServer(next.Section("server"))
			if err != nil {
				t.Fatal(err)
			}
			if server.URL != "https://server/jellyfin" || server.Jellyfin.APIKey != "private-key" || server.Jellyfin.Username != "viewer" || !server.InsecureTLS || server.Transcode != (settings.Transcode{MaxWidth: 640, MaxHeight: 480, VideoBitrate: 8000000}) {
				t.Fatal("migration lost connection options")
			}
			var diagnostics struct{ Enabled bool }
			if err := next.Section("diagnostics").Decode(&diagnostics); err != nil || !diagnostics.Enabled {
				t.Fatal("legacy DEBUGLOG not migrated")
			}
			after, err := os.ReadFile(legacy)
			if err != nil || string(after) != original {
				t.Fatal("legacy file changed")
			}
			if err := os.Remove(legacy); err != nil {
				t.Fatal(err)
			}
			if _, err := serverConnector(next, legacy, dir, "test", nil); err != nil {
				t.Fatal("JSON still needs legacy file")
			}
		})
	}
}

func TestUnifiedJellyfinConnectsWithoutLegacyFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Authorization"), `Token="private-key"`) {
			t.Error("API-key credential missing")
		}
		switch r.URL.Path {
		case "/Users":
			fmt.Fprint(w, `[{"Id":"viewer-id","Name":"viewer"}]`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	body := `{"server":{"url":"` + server.URL + `","jellyfin":{"api_key":"private-key","username":"viewer"}}}`
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	source, err := settings.Load(path, true)
	if err != nil {
		t.Fatal(err)
	}
	connector, err := serverConnector(source, filepath.Join(dir, "does-not-exist.conf"), dir, "test", nil)
	if err != nil {
		t.Fatal(err)
	}
	session, err := connector.Connect(t.Context(), connection.Interaction{})
	if err != nil {
		t.Fatal(err)
	}
	if session.Server.Identity().Server != server.URL || session.Server.Identity().User != "viewer-id" {
		t.Fatal("wrong authenticated identity")
	}
}
