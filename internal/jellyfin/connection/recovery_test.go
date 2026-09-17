package connection

import (
	"bytes"
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
	"mistervision/internal/diagnostics"
	"mistervision/internal/jellyfin"
	"mistervision/internal/serverstate"
)

// recoveryFixture models a remembered address whose transport stops responding.
// The new endpoint checks that identity probes never carry saved credentials.
type recoveryFixture struct {
	connector             Connector
	old, next             connection.Server
	original              jellyfin.Session
	approved              atomic.Bool
	probes, authenticated atomic.Int32
	scans                 int
}

func newRecoveryFixture(t *testing.T, publicID string, oldStatus int) *recoveryFixture {
	t.Helper()
	f := &recoveryFixture{}
	old := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if oldStatus != 0 {
			if r.URL.Path == "/QuickConnect/Enabled" {
				fmt.Fprint(w, "false")
				return
			}
			w.WriteHeader(oldStatus)
			return
		}
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			conn.Close()
		}
	}))
	t.Cleanup(old.Close)
	next := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/System/Info/Public":
			f.probes.Add(1)
			if r.Header.Get("Authorization") != "" || r.Header.Get("X-Emby-Token") != "" || r.URL.RawQuery != "" {
				t.Error("identity probe sent credentials")
			}
			json.NewEncoder(w).Encode(map[string]string{"Id": publicID})
		case "/UserViews":
			if !f.approved.Load() {
				t.Error("credentials sent before address confirmation")
			}
			if !strings.Contains(r.Header.Get("Authorization"), `Token="private-token"`) {
				t.Error("saved token was not preserved")
			}
			f.authenticated.Add(1)
			fmt.Fprint(w, `{"Items":[]}`)
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
			w.WriteHeader(500)
		}
	}))
	t.Cleanup(next.Close)
	f.old = connection.Server{ID: "stable-id", Name: "Original", URL: old.URL}
	f.next = connection.Server{ID: "stable-id", Name: "Renamed server", URL: next.URL}
	dir := t.TempDir()
	f.original = jellyfin.Session{Server: old.URL, DeviceID: "installation-id", Token: "private-token", UserID: "viewer"}
	if err := serverstate.SaveServer(filepath.Join(dir, "jellyfin-server.json"), f.old); err != nil {
		t.Fatal(err)
	}
	if err := jellyfin.SaveSession(dir, f.original); err != nil {
		t.Fatal(err)
	}
	f.connector = Connector{StateDir: dir, ConfigPath: filepath.Join(dir, "jellyfin.conf"), Discovery: discoverFunc(func(context.Context) ([]connection.Server, error) {
		f.scans++
		return []connection.Server{f.next}, nil
	})}
	return f
}

func (f *recoveryFixture) accept(t *testing.T) connection.Interaction {
	return connection.Interaction{ChooseServer: func(ctx context.Context, servers []connection.Server) (connection.Server, error) {
		if len(servers) != 1 || servers[0] != f.next || f.probes.Load() == 0 {
			t.Error("unverified or wrong recovery candidate")
		}
		f.approved.Store(true)
		return servers[0], nil
	}}
}

func TestAddressRecoveryPreservesSignInAndRemembersNewAddress(t *testing.T) {
	f := newRecoveryFixture(t, "stable-id", 0)
	path := filepath.Join(t.TempDir(), "diagnostics.log")
	log, err := diagnostics.Open(diagnostics.Config{Enabled: true, Path: path, MaxBytes: 8192})
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	f.connector.Diagnostics = log
	interaction := f.accept(t)
	offered := false
	interaction.Progress = func(p connection.Presentation) {
		if p.Title == "Server address changed" {
			offered = p.Kind == connection.SetupServers && p.Message != ""
		}
	}
	for range 2 {
		result, err := f.connector.Connect(t.Context(), interaction)
		if err != nil || result.Server == nil || result.Server.Identity().Server != f.next.URL {
			t.Fatalf("recovery: %v", err)
		}
	}
	if !offered || f.scans != 1 || f.authenticated.Load() != 2 {
		t.Fatal("recovery repeated or missed confirmation")
	}
	saved, err := serverstate.LoadServer(filepath.Join(f.connector.StateDir, "jellyfin-server.json"))
	if err != nil || saved != f.next {
		t.Fatal("new address not remembered")
	}
	session, _, err := jellyfin.LoadSession(f.connector.StateDir, f.next.URL)
	if err != nil || session.Token != f.original.Token || session.DeviceID != f.original.DeviceID || session.UserID != f.original.UserID || session.ServerID != f.next.ID {
		t.Fatal("sign-in changed during recovery")
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("connection.address-recovered")) {
		t.Fatal("successful recovery not logged")
	}
	for _, secret := range []string{f.original.Token, f.old.URL, f.next.URL, f.original.DeviceID} {
		if bytes.Contains(data, []byte(secret)) {
			t.Fatal("recovery diagnostics leaked private state")
		}
	}
}

func TestAddressRecoveryFailurePreservesOriginalState(t *testing.T) {
	for _, outcome := range []string{"empty", "different-id", "public-id-mismatch", "cancel", "unoffered", "network", "explicit-json", "explicit-legacy", "http-error", "rejected", "session-save"} {
		t.Run(outcome, func(t *testing.T) {
			publicID, oldStatus := "stable-id", 0
			if outcome == "public-id-mismatch" {
				publicID = "different-id"
			}
			if outcome == "http-error" {
				oldStatus = 503
			}
			if outcome == "rejected" {
				oldStatus = 401
			}
			f := newRecoveryFixture(t, publicID, oldStatus)
			if outcome == "empty" || outcome == "network" || outcome == "different-id" {
				f.connector.Discovery = discoverFunc(func(context.Context) ([]connection.Server, error) {
					f.scans++
					if outcome == "network" {
						return nil, errors.New("private transport detail")
					}
					if outcome == "different-id" {
						candidate := f.next
						candidate.ID = "other"
						return []connection.Server{candidate}, nil
					}
					return nil, nil
				})
			}
			if outcome == "explicit-json" {
				f.connector.Config = &jellyfin.Config{Server: f.old.URL}
			}
			if outcome == "explicit-legacy" {
				if err := os.WriteFile(f.connector.ConfigPath, []byte(f.old.URL), 0600); err != nil {
					t.Fatal(err)
				}
			}
			before := make(map[string][]byte)
			for _, name := range []string{"session.json", "jellyfin-server.json"} {
				data, err := os.ReadFile(filepath.Join(f.connector.StateDir, name))
				if err != nil {
					t.Fatal(err)
				}
				before[name] = data
			}
			interaction := f.accept(t)
			choose := interaction.ChooseServer
			interaction.ChooseServer = func(ctx context.Context, servers []connection.Server) (connection.Server, error) {
				if outcome == "cancel" {
					return connection.Server{}, context.Canceled
				}
				if outcome == "unoffered" {
					return f.old, nil
				}
				if outcome == "session-save" {
					if err := os.Chmod(f.connector.StateDir, 0500); err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { os.Chmod(f.connector.StateDir, 0700) })
				}
				return choose(ctx, servers)
			}
			if outcome == "session-save" && os.Geteuid() == 0 {
				t.Skip("permission test requires an unprivileged process")
			}
			_, err := f.connector.Connect(t.Context(), interaction)
			if err == nil {
				t.Fatal("failed recovery succeeded")
			}
			if outcome == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation was lost")
			}
			if strings.Contains(f.connector.Describe(err).Message, "private") {
				t.Fatal("unsafe error presentation")
			}
			for name, data := range before {
				after, err := os.ReadFile(filepath.Join(f.connector.StateDir, name))
				if err != nil || !bytes.Equal(data, after) {
					t.Fatalf("failed recovery changed %s", name)
				}
			}
			if outcome != "session-save" && f.authenticated.Load() != 0 {
				t.Fatal("failed recovery sent saved credentials")
			}
			if (strings.HasPrefix(outcome, "explicit-") || outcome == "http-error" || outcome == "rejected") && f.scans != 0 {
				t.Fatal("recovery ran for authoritative config or reachable server")
			}
		})
	}
}

func TestRecoveryAfterInterruptedAddressSave(t *testing.T) {
	f := newRecoveryFixture(t, "stable-id", 0)
	// The first save succeeded, but the address record still points to the old URL.
	staged := f.original
	staged.Server, staged.ServerID = f.next.URL, f.next.ID
	if err := jellyfin.SaveSession(f.connector.StateDir, staged); err != nil {
		t.Fatal(err)
	}
	if _, err := f.connector.Connect(t.Context(), f.accept(t)); err != nil {
		t.Fatalf("interrupted save lost sign-in: %v", err)
	}
	if f.authenticated.Load() != 1 {
		t.Fatal("stored token was not used")
	}
}

func TestRecoveryDoesNotDowngradeHTTPS(t *testing.T) {
	f := newRecoveryFixture(t, "stable-id", 0)
	old := f.old
	old.URL = "https://old.example"
	_, err := f.connector.recoverAddress(t.Context(), f.accept(t), jellyfin.Config{Server: old.URL}, old, f.original, false)
	if err == nil || f.probes.Load() != 0 || f.authenticated.Load() != 0 {
		t.Fatal("HTTPS recovery offered an HTTP endpoint")
	}
}

func TestRecoveryCancellationStopsDiscovery(t *testing.T) {
	f := newRecoveryFixture(t, "stable-id", 0)
	ctx, cancel := context.WithCancel(t.Context())
	f.connector.Discovery = discoverFunc(func(context.Context) ([]connection.Server, error) {
		cancel()
		return nil, ctx.Err()
	})
	_, err := f.connector.Connect(ctx, f.accept(t))
	if !errors.Is(err, context.Canceled) || f.authenticated.Load() != 0 {
		t.Fatal("canceled discovery continued")
	}
	saved, _, err := jellyfin.LoadSession(f.connector.StateDir, f.old.URL)
	if err != nil || saved != f.original {
		t.Fatal("cancellation changed sign-in")
	}
}
