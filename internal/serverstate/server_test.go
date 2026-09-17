package serverstate

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mistervision/internal/connection"
)

func TestRememberedServerRoundTripAndFailures(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "jellyfin-server.json")
	server := connection.Server{ID: "id", Name: "Living room", URL: "https://example.test/jellyfin"}
	if _, err := LoadServer(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if err := SaveServer(path, server); err != nil {
		t.Fatal(err)
	}
	got, err := LoadServer(path)
	if err != nil || got != server {
		t.Fatalf("round trip: %v %v", got, err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatalf("permissions: %v", info.Mode())
	}
	bad := server
	bad.URL = "http://user:secret@example.test"
	if err := SaveServer(path, bad); err == nil {
		t.Fatal("saved credentials in address")
	}
	if got, err := LoadServer(path); err != nil || got != server {
		t.Fatal("failed save replaced previous selection")
	}
	for _, body := range []string{"{", strings.Repeat("x", 16385), `{"id":"x","name":"Server","url":"file:///etc/passwd"}`} {
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadServer(path); err == nil {
			t.Fatal("accepted damaged state")
		}
		data, _ := os.ReadFile(path)
		if string(data) != body {
			t.Fatal("damaged state was overwritten")
		}
	}
	if err := SaveServer(filepath.Dir(path), server); err == nil {
		t.Fatal("replaced directory with selection")
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".server-") {
			t.Fatal("temporary file leaked")
		}
	}
}
