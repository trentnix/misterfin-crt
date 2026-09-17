package serverstate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConnectionChoicePersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "connection-choice.json")
	choice := Choice{ID: "profile/plex", Configuration: strings.Repeat("a", 64)}
	if err := SaveChoice(path, choice); err != nil {
		t.Fatal(err)
	}
	if got, err := LoadChoice(path); err != nil || got != choice {
		t.Fatalf("choice: %+v %v", got, err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm()&0077 != 0 {
		t.Fatal("choice must be private")
	}
	for _, data := range []string{"broken", strings.Repeat("x", 1025), `{"ID":"x"}`} {
		os.WriteFile(path, []byte(data), 0600)
		if _, err := LoadChoice(path); err == nil {
			t.Fatal("invalid choice accepted")
		}
		got, _ := os.ReadFile(path)
		if string(got) != data {
			t.Fatal("damaged file was overwritten")
		}
	}
}
