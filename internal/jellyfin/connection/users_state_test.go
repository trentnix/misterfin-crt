package connection

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"mistervision/internal/connection"
)

func TestInvalidUserStorePreservesActiveSignIn(t *testing.T) {
	for _, kind := range []string{"malformed", "oversized", "incomplete", "duplicate", "directory"} {
		t.Run(kind, func(t *testing.T) {
			f := newUserFixture(t)
			path := filepath.Join(f.connector.StateDir, "jellyfin-users.json")
			data := []byte("{")
			switch kind {
			case "oversized":
				data = bytes.Repeat([]byte(" "), maxUserStateBytes+1)
			case "incomplete":
				data = []byte(`[{"user":{"Id":"one","Name":"Viewer"}}]`)
			case "duplicate":
				data, _ = json.Marshal(userStore{f.users[0], f.users[0]})
			case "directory":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			}
			if kind != "directory" {
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			activePath := filepath.Join(f.connector.StateDir, "session.json")
			before, err := os.ReadFile(activePath)
			if err != nil {
				t.Fatal(err)
			}
			result, err := f.connector.Connect(t.Context(), connection.Interaction{})
			if err == nil || result.Server != nil || f.connector.Describe(err).Title != connection.SignInStorageTitle {
				t.Fatal("invalid store did not produce storage guidance")
			}
			after, err := os.ReadFile(activePath)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("invalid store replaced active sign-in")
			}
			if kind != "directory" {
				after, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(data, after) {
					t.Fatal("invalid roster was overwritten")
				}
			}
		})
	}
}

func TestUnchangedUserDoesNotRewriteState(t *testing.T) {
	f := newUserFixture(t)
	path := filepath.Join(f.connector.StateDir, "jellyfin-users.json")
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.connector.Connect(t.Context(), connection.Interaction{}); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil || !os.SameFile(before, after) {
		t.Fatal("unchanged sign-in rewrote the user store")
	}
}
