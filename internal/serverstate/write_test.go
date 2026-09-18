package serverstate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFileReplacesPrivatelyAndCleansFailedRename(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "private", "state.json")
	for _, data := range []string{"original", "replacement"} {
		if err := WriteFile(path, []byte(data)); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(path)
		if err != nil || string(got) != data {
			t.Fatal("replacement not published")
		}
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatal("state is not private")
		}
	}
	blocked := filepath.Join(dir, "blocked")
	if err := os.Mkdir(blocked, 0700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(blocked, "preserve")
	if err := os.WriteFile(marker, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(blocked, []byte("new")); err == nil {
		t.Fatal("replaced a directory")
	}
	if data, err := os.ReadFile(marker); err != nil || string(data) != "old" {
		t.Fatal("failed write damaged existing state")
	}
	leftovers, err := filepath.Glob(filepath.Join(dir, ".state-*"))
	if err != nil || len(leftovers) != 0 {
		t.Fatal("failed write leaked temporary state")
	}
}

func TestLoadingAnotherServerDoesNotPersistEmptySession(t *testing.T) {
	dir := t.TempDir()
	original := Session{Server: "http://old", DeviceID: "device", Token: "token", UserID: "user"}
	if err := SaveSession(dir, original); err != nil {
		t.Fatal(err)
	}
	next, _, err := LoadSession(dir, "http://new")
	if err != nil || next.Server != "http://new" || next.Token != "" {
		t.Fatal("new session reused old credentials")
	}
	saved, _, err := LoadSession(dir, "http://old")
	if err != nil || saved != original {
		t.Fatal("loading new server replaced working credentials")
	}
}
