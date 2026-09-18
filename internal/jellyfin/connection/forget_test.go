package connection

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mistervision/internal/connection"
	"mistervision/internal/jellyfin"
)

func TestForgetJellyfinUserRequiresConfirmation(t *testing.T) {
	for _, outcome := range []string{"decline", "missing", "canceled", "roster failure"} {
		t.Run(outcome, func(t *testing.T) {
			f := newUserFixture(t)
			dir := f.connector.StateDir
			path := filepath.Join(dir, "session.json")
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			i := connection.Interaction{ProfileAction: connection.ProfileForget}
			if outcome != "missing" {
				i.Confirm = func(context.Context, connection.Confirmation) (bool, error) {
					if outcome == "canceled" {
						cancel()
						return true, nil
					}
					if outcome == "roster failure" {
						roster := filepath.Join(dir, "jellyfin-users.json")
						if err := os.Rename(roster, roster+".previous"); err != nil {
							t.Fatal(err)
						}
						if err := os.Mkdir(roster, 0700); err != nil {
							t.Fatal(err)
						}
						return true, nil
					}
					return false, nil
				}
			}
			result, err := f.connector.Connect(ctx, i)
			if err == nil || errors.Is(err, connection.ErrSignedOut) || result.Server != nil {
				t.Fatal("unconfirmed/failed removal signed out")
			}
			after, err := os.ReadFile(path)
			if err != nil || string(after) != string(before) {
				t.Fatal("active sign-in changed")
			}
		})
	}
}

func TestForgetInactiveJellyfinUserUpdatesAvailableAction(t *testing.T) {
	f := newUserFixture(t)
	prompts := 0
	result, err := f.connector.Connect(t.Context(), connection.Interaction{
		ProfileAction: connection.ProfileChoose,
		ChooseProfile: func(_ context.Context, p connection.ProfilePrompt) (connection.ProfileSelection, error) {
			prompts++
			if !p.Forget {
				t.Fatal("missing removal action")
			}
			return connection.ProfileSelection{ID: "two", Action: connection.ProfileForget}, nil
		},
		Confirm: func(_ context.Context, p connection.Confirmation) (bool, error) {
			if !strings.Contains(p.Message, "Viewer two") {
				t.Fatal("wrong user in confirmation")
			}
			return true, nil
		},
	})
	if err != nil || prompts != 1 || result.Server.Identity().User != "one" || result.ProfileAction != connection.ProfileAdd {
		t.Fatalf("picker did not resume: %v", err)
	}
	users, err := loadUsers(f.connector.StateDir)
	if err != nil || len(users) != 1 || users[0].Session.Token != "token-one" {
		t.Fatal("wrong credentials removed")
	}
}

func TestForgetInactiveUserRebuildsPickerAndPreservesOtherUsers(t *testing.T) {
	for _, accept := range []bool{false, true} {
		f := newUserFixture(t)
		third := f.users[1]
		third.User.ID, third.User.Name = "three", "Viewer three"
		third.Session.UserID, third.Session.Token, third.Session.DeviceID = "three", "token-three", "device-three"
		f.users = append(f.users, third)
		if err := f.users.save(f.connector.StateDir); err != nil {
			t.Fatal(err)
		}
		prompts := 0
		result, err := f.connector.Connect(t.Context(), connection.Interaction{
			ProfileAction: connection.ProfileChoose,
			ChooseProfile: func(_ context.Context, p connection.ProfilePrompt) (connection.ProfileSelection, error) {
				prompts++
				if prompts == 1 {
					return connection.ProfileSelection{ID: "two", Action: connection.ProfileForget}, nil
				}
				want := 3
				if accept {
					want = 2
				}
				if len(p.Profiles) != want || p.Profiles[len(p.Profiles)-1].ID != "three" {
					t.Fatal("picker did not preserve the remaining users")
				}
				return connection.ProfileSelection{ID: "three"}, nil
			},
			Confirm: func(context.Context, connection.Confirmation) (bool, error) { return accept, nil },
		})
		if err != nil || prompts != 2 || result.Server.Identity().User != "three" {
			t.Fatalf("could not select another user after confirmation: %v", err)
		}
		users, err := loadUsers(f.connector.StateDir)
		if err != nil {
			t.Fatal(err)
		}
		_, found := users.find(f.users[0].Session, "two")
		if found == accept {
			t.Fatal("stored users did not match the confirmation decision")
		}
	}
}

func TestForgetActiveJellyfinUserCannotReturnThroughStaleSession(t *testing.T) {
	for _, interrupted := range []bool{false, true} {
		f := newUserFixture(t)
		path := filepath.Join(f.connector.StateDir, "session.json")
		_, err := f.connector.Connect(t.Context(), connection.Interaction{ProfileAction: connection.ProfileForget, Confirm: func(context.Context, connection.Confirmation) (bool, error) {
			if interrupted {
				if err := os.Rename(path, path+".previous"); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			}
			return true, nil
		}})
		if !errors.Is(err, connection.ErrSignedOut) {
			t.Fatalf("active removal: %v", err)
		}
		if interrupted {
			if err == connection.ErrSignedOut {
				t.Fatal("cleanup failure was hidden")
			}
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(path+".previous", path); err != nil {
				t.Fatal(err)
			}
		}
		result, err := f.connector.Connect(t.Context(), connection.Interaction{ChooseProfile: func(_ context.Context, p connection.ProfilePrompt) (connection.ProfileSelection, error) {
			if len(p.Profiles) != 1 || p.Profiles[0].ID != "two" {
				t.Fatal("forgotten active user was restored")
			}
			return connection.ProfileSelection{ID: "two"}, nil
		}})
		if err != nil || result.Server.Identity().User != "two" {
			t.Fatalf("remaining user did not open: %v", err)
		}
	}
}

func TestForgetLastJellyfinUserRequiresQuickConnect(t *testing.T) {
	f := newUserFixture(t)
	if err := (userStore{f.users[0]}).save(f.connector.StateDir); err != nil {
		t.Fatal(err)
	}
	_, err := f.connector.Connect(t.Context(), connection.Interaction{ProfileAction: connection.ProfileForget, Confirm: func(context.Context, connection.Confirmation) (bool, error) { return true, nil }})
	if !errors.Is(err, connection.ErrSignedOut) {
		t.Fatal(err)
	}
	users, err := loadUsers(f.connector.StateDir)
	if err != nil || users == nil || len(users) != 0 {
		t.Fatal("empty authoritative roster lost")
	}
	saved, _, err := jellyfin.LoadSession(f.connector.StateDir, f.connector.Config.Server)
	if err != nil || saved.Token != "" || saved.UserID != "" {
		t.Fatal("last user's credentials remain active")
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	code := false
	_, err = f.connector.Connect(ctx, connection.Interaction{Progress: func(p connection.Presentation) {
		if p.Kind == connection.SetupApproval {
			code = true
			cancel()
		}
	}})
	if !code || !errors.Is(err, context.Canceled) {
		t.Fatalf("last-user removal did not request authorization: %v", err)
	}
}

func TestJellyfinReauthorizationChecksChangedIdentity(t *testing.T) {
	for _, accept := range []bool{false, true} {
		f := newUserFixture(t)
		f.reject = "one"
		f.approved = "two"
		path := filepath.Join(f.connector.StateDir, "session.json")
		before, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		prompted, explained := false, false
		result, err := f.connector.Connect(t.Context(), connection.Interaction{
			Progress: func(p connection.Presentation) {
				if p.Kind == connection.SetupApproval {
					explained = strings.Contains(p.Message, "Viewer one") && strings.Contains(p.Message, "expired")
				}
			},
			Confirm: func(_ context.Context, p connection.Confirmation) (bool, error) {
				prompted = strings.Contains(p.Message, "Viewer one") && strings.Contains(p.Message, "Viewer two")
				return accept, nil
			},
		})
		if !prompted || !explained {
			t.Fatal("identity change or reauthorization was not explained")
		}
		if accept {
			if err != nil || result.Server.Identity().User != "two" {
				t.Fatalf("confirmed identity: %v", err)
			}
		} else {
			if !errors.Is(err, connection.ErrCanceled) {
				t.Fatal(err)
			}
			after, err := os.ReadFile(path)
			if err != nil || string(after) != string(before) {
				t.Fatal("declined identity replaced active sign-in")
			}
		}
	}
}

func TestCancelForgetPreservesHighlightedUserAndCredentials(t *testing.T) {
	f := newUserFixture(t)
	before := make(map[string]string)
	for _, name := range []string{"session.json", "jellyfin-users.json"} {
		data, err := os.ReadFile(filepath.Join(f.connector.StateDir, name))
		if err != nil {
			t.Fatal(err)
		}
		before[name] = string(data)
	}
	stop := errors.New("leave picker")
	calls := 0
	choices := []string{"two", "one", "two"}
	_, err := f.connector.Connect(t.Context(), connection.Interaction{
		ProfileAction: connection.ProfileChoose,
		ChooseProfile: func(_ context.Context, p connection.ProfilePrompt) (connection.ProfileSelection, error) {
			if calls > 0 && p.Profiles[p.Selected].ID != choices[calls-1] {
				t.Errorf("cancel moved highlight from %s to %s", choices[calls-1], p.Profiles[p.Selected].ID)
			}
			if calls == len(choices) {
				return connection.ProfileSelection{}, stop
			}
			id := choices[calls]
			calls++
			return connection.ProfileSelection{ID: id, Action: connection.ProfileForget}, nil
		},
		Confirm: func(context.Context, connection.Confirmation) (bool, error) { return false, nil },
	})
	if !errors.Is(err, stop) || calls != len(choices) {
		t.Fatalf("wrong cancel route: calls=%d err=%v", calls, err)
	}
	for name, original := range before {
		data, err := os.ReadFile(filepath.Join(f.connector.StateDir, name))
		if err != nil || string(data) != original {
			t.Fatalf("cancel changed %s", name)
		}
	}
}
