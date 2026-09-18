package plex

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

	"mistervision/internal/connection"
	"mistervision/internal/serverstate"
)

func TestPlexSignOutRequiresConfirmationAndBlocksLegacyFallback(t *testing.T) {
	for _, outcome := range []string{"decline", "missing", "canceled", "confirmed", "storage failure"} {
		t.Run(outcome, func(t *testing.T) {
			f := newDiscoveryFixture(t, true)
			if _, err := f.connector.connectDiscovered(t.Context(), connection.Interaction{ChooseServer: chooseFirst}, &serverDiscovery{account: f.account}); err != nil {
				t.Fatal(err)
			}
			dir := StateDir(f.connector.StateDir)
			path := filepath.Join(dir, "server.json")
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			i := connection.Interaction{ProfileAction: connection.ProfileForget}
			if outcome != "missing" {
				i.Confirm = func(_ context.Context, p connection.Confirmation) (bool, error) {
					if !strings.Contains(p.Message, "Test Viewer") {
						t.Fatal("confirmation lost account name")
					}
					if outcome == "canceled" {
						cancel()
						return true, nil
					}
					if outcome == "storage failure" {
						if err := os.Rename(path, path+".previous"); err != nil {
							t.Fatal(err)
						}
						if err := os.Mkdir(path, 0700); err != nil {
							t.Fatal(err)
						}
					}
					return outcome != "decline", nil
				}
			}
			_, err = f.connector.Connect(ctx, i)
			if outcome != "confirmed" {
				if err == nil || errors.Is(err, connection.ErrSignedOut) {
					t.Fatal("unconfirmed or failed removal signed out")
				}
				if outcome == "storage failure" {
					path += ".previous"
				}
				after, e := os.ReadFile(path)
				if e != nil || string(after) != string(before) {
					t.Fatal("failed removal changed account")
				}
				return
			}
			if !errors.Is(err, connection.ErrSignedOut) {
				t.Fatal(err)
			}
			state, err := loadDiscoveryState(dir)
			if err != nil || !state.SignedOut || state.Account != nil || state.Credentials != nil || state.Profile != nil {
				t.Fatal("credentials retained in signed-out record")
			}
			// Simulate old migration files surviving interrupted cleanup. Neither the
			// configured-server nor discovery loader may restore these credentials.
			stale := serverstate.Session{Server: f.account.accountURL, Token: "old-token", UserID: "7", DeviceID: "old-device"}
			if err := serverstate.SaveSession(filepath.Join(dir, "account"), stale); err != nil {
				t.Fatal(err)
			}
			stale.Server = f.server.URL
			if err := serverstate.SaveSession(dir, stale); err != nil {
				t.Fatal(err)
			}
			for _, address := range []string{"", f.server.URL} {
				f.connector.Config.Server = address
				saved, _, err := f.connector.loadAccount(f.account.accountURL)
				if err != nil || saved.Token != "" || saved.UserID != "" || saved.DeviceID == "" {
					t.Fatal("sign-out resurrected legacy credentials")
				}
			}
			if _, err := f.connector.Connect(t.Context(), connection.Interaction{ProfileAction: connection.ProfileForget}); !errors.Is(err, connection.ErrSignedOut) {
				t.Fatal("cleanup retry required another approval")
			}
			if _, err := os.Stat(filepath.Join(dir, "account")); !os.IsNotExist(err) {
				t.Fatal("legacy account was not removed")
			}
			if _, err := os.Stat(filepath.Join(dir, "session.json")); !os.IsNotExist(err) {
				t.Fatal("legacy configured token was not removed")
			}
		})
	}
}

func TestPlexReauthorizationChecksChangedAccount(t *testing.T) {
	for _, accept := range []bool{false, true} {
		f := newDiscoveryFixture(t, true)
		if _, err := f.connector.connectDiscovered(t.Context(), connection.Interaction{ChooseServer: chooseFirst}, &serverDiscovery{account: f.account}); err != nil {
			t.Fatal(err)
		}
		dir := StateDir(f.connector.StateDir)
		state, err := loadDiscoveryState(dir)
		if err != nil {
			t.Fatal(err)
		}
		state.Account.Token = "expired"
		if err := saveDiscoveryState(dir, state); err != nil {
			t.Fatal(err)
		}
		before, err := os.ReadFile(filepath.Join(dir, "server.json"))
		if err != nil {
			t.Fatal(err)
		}
		f.rejectAccount.Store(true)
		f.newAccount.Store(true)
		prompted, explained := false, false
		result, err := f.connector.connectDiscovered(t.Context(), connection.Interaction{SelectServer: true, ChooseServer: chooseFirst,
			Progress: func(p connection.Presentation) {
				if p.Kind == connection.SetupApproval {
					explained = strings.Contains(p.Message, "Test Viewer") && strings.Contains(p.Message, "expired")
				}
			},
			Confirm: func(_ context.Context, p connection.Confirmation) (bool, error) {
				prompted = strings.Contains(p.Message, "Test Viewer") && strings.Contains(p.Message, "Another Viewer")
				return accept, nil
			},
		}, &serverDiscovery{account: f.account})
		if !prompted || !explained {
			t.Fatal("account change or expired sign-in was not explained")
		}
		if accept {
			if err != nil || result.Server.Identity().User != "8" {
				t.Fatalf("confirmed account: %v", err)
			}
		} else {
			if !errors.Is(err, connection.ErrCanceled) {
				t.Fatal(err)
			}
			after, e := os.ReadFile(filepath.Join(dir, "server.json"))
			if e != nil || string(after) != string(before) {
				t.Fatal("declined account replaced sign-in")
			}
		}
	}
}

func TestMissingHomeProfileRequiresSelectionEvenWithOneRemaining(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/home/users" {
			fmt.Fprint(w, `<MediaContainer><User id="7" title="Parent" protected="0"/></MediaContainer>`)
			return
		}
		calls++
		fmt.Fprint(w, `<user id="7" authenticationToken="viewer-token"/>`)
	}))
	defer server.Close()
	client := NewClient(Config{}, serverstate.Session{Token: "owner", UserID: "7", DeviceID: "device"})
	client.accountURL = server.URL
	d := serverDiscovery{account: client}
	err := d.chooseHome(t.Context(), connection.Interaction{ChooseProfile: func(_ context.Context, p connection.ProfilePrompt) (connection.ProfileSelection, error) {
		if len(p.Profiles) != 1 || p.Message != messageProfileUnavailable {
			t.Fatal("missing profile did not explain the new choice")
		}
		return connection.ProfileSelection{}, context.Canceled
	}}, accountIdentity{ID: 7, Home: true}, "removed-child")
	if !errors.Is(err, context.Canceled) || calls != 0 {
		t.Fatal("removed viewer silently became remaining viewer")
	}
	err = d.chooseHome(t.Context(), connection.Interaction{}, accountIdentity{ID: 7}, "removed-child")
	if !errors.Is(err, connection.ErrCanceled) {
		t.Fatal("removed Home silently became owner account")
	}
}
