package connection

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"mistervision/internal/connection"
	"mistervision/internal/jellyfin"
)

func TestAPIKeyKeepsDeviceIdentityWithoutSavingKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/Users" {
			t.Errorf("unexpected request: %s", r.URL.Path)
		}
		fmt.Fprint(w, `[{"Id":"user","Name":"viewer"}]`)
	}))
	defer server.Close()
	config := jellyfin.Config{Server: server.URL, APIKey: "test-api-key", Username: "viewer"}
	dir := t.TempDir()
	first := ""
	for range 2 {
		c := &Connector{Config: &config, StateDir: dir}
		result, err := c.Connect(t.Context(), connection.Interaction{})
		if err != nil {
			t.Fatal(err)
		}
		session := result.Server.(*jellyfin.Client).Session
		if session.Token != config.APIKey || session.UserID != "user" || session.DeviceID == "" {
			t.Fatal("API-key authentication failed")
		}
		if first != "" && session.DeviceID != first {
			t.Fatal("restart changed device identity")
		}
		first = session.DeviceID
		saved, _, err := jellyfin.LoadSession(dir, config.Server)
		if err != nil || saved.DeviceID != first || saved.Token != "" || saved.UserID != "" {
			t.Fatal("device identity missing or API credentials persisted")
		}
	}
}

func TestAPIKeyFailurePreservesSavedIdentity(t *testing.T) {
	for _, outcome := range []string{"rejected", "missing user", "canceled", "storage failure"} {
		t.Run(outcome, func(t *testing.T) {
			dir := t.TempDir()
			original := jellyfin.Session{Server: "http://previous", DeviceID: "previous-device", Token: "previous-token", UserID: "previous-user"}
			if err := jellyfin.SaveSession(dir, original); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "session.json")
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch outcome {
				case "rejected":
					w.WriteHeader(http.StatusUnauthorized)
				case "missing user":
					fmt.Fprint(w, `[]`)
				case "canceled":
					cancel()
					fmt.Fprint(w, `[{"Id":"user","Name":"viewer"}]`)
				case "storage failure":
					// Simulate storage becoming unwritable after loading the prior identity.
					if err := os.Rename(path, path+".previous"); err != nil {
						t.Error(err)
					}
					if err := os.Mkdir(path, 0700); err != nil {
						t.Error(err)
					}
					fmt.Fprint(w, `[{"Id":"user","Name":"viewer"}]`)
				}
			}))
			defer server.Close()
			config := jellyfin.Config{Server: server.URL, APIKey: "test-api-key", Username: "viewer"}
			c := &Connector{Config: &config, StateDir: dir}
			result, err := c.Connect(ctx, connection.Interaction{})
			if err == nil || result.Server != nil {
				t.Fatal("failed authentication or persistence returned a session")
			}
			if outcome == "storage failure" {
				path += ".previous"
			}
			after, err := os.ReadFile(path)
			if err != nil || string(after) != string(before) {
				t.Fatal("failed attempt replaced working sign-in")
			}
		})
	}
}
