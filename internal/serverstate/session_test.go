package serverstate

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDamagedSessionIsPreservedAndReplaced(t *testing.T) {
	for _, data := range []string{`{"Token":"private",`, strings.Repeat(" ", 64<<10) + `{}`} {
		dir := t.TempDir()
		path := filepath.Join(dir, "session.json")
		if err := os.WriteFile(path, []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
		session, recovered, err := LoadSession(dir, "http://server")
		if err != nil || !recovered || session.DeviceID == "" || session.Token != "" {
			t.Fatalf("recovery: %+v %v %v", session, recovered, err)
		}
		backups, err := filepath.Glob(filepath.Join(dir, "session-damaged-*"))
		if err != nil || len(backups) != 1 {
			t.Fatal("damaged session was not preserved once")
		}
		saved, err := os.ReadFile(backups[0])
		if err != nil || !bytes.Equal(saved, []byte(data)) {
			t.Fatal("backup changed damaged data")
		}
		info, err := os.Stat(backups[0])
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatal("backup is not private")
		}
		next, again, err := LoadSession(dir, "http://server")
		if err != nil || again || next != session {
			t.Fatal("retry replaced the repaired session")
		}
	}
}

func TestSessionStorageFailureDoesNotReplaceRecord(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.json")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(path, "preserve")
	if err := os.WriteFile(marker, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, recovered, err := LoadSession(dir, "http://server"); err == nil || recovered {
		t.Fatal("storage error treated as recoverable corruption")
	}
	if data, err := os.ReadFile(marker); err != nil || string(data) != "original" {
		t.Fatal("storage error damaged original")
	}
}
