package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// loadTitle exercises legacy file reading through the authoritative UI schema.
func loadTitle(path string) (*string, error) {
	ui := ParseUI(Read(path, 4096, false))
	return ui.Title, ui.TitleError
}

func TestUIHeading(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ui.json")
	if title, err := loadTitle(path); title != nil || err != nil {
		t.Fatalf("missing config: %v, %v", title, err)
	}
	for _, data := range []string{`{}`, `{"title":null}`} {
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if title, err := loadTitle(path); title != nil || err != nil {
			t.Fatalf("unset title: %v, %v", title, err)
		}
	}
	for _, tc := range []struct {
		data, want string
		invalid    bool
	}{
		{`{"title":""}`, "", false},
		{`{"title":"  Trent's CRT  "}`, "Trent's CRT", false},
		{`{"title":"First\n\tSecond\u0000"}`, "First Second", false},
		{`{"title":"  \u0000  "}`, "", false},
		{`{"title":"Café"}`, "Café", false},
		{`{"title":"` + strings.Repeat("W", 1000) + `"}`, strings.Repeat("W", 1000), false},
		{`null`, "", true},
		{`[]`, "", true},
		{`{"unknown":true}`, "", true},
		{`{"title":4}`, "", true},
		{`{} {}`, "", true},
		{strings.Repeat(" ", 4097), "", true},
	} {
		if err := os.WriteFile(path, []byte(tc.data), 0600); err != nil {
			t.Fatal(err)
		}
		got, err := loadTitle(path)
		if (err != nil) != tc.invalid {
			t.Fatalf("config %q: unexpected error: %v", tc.data, err)
		}
		if tc.invalid && got != nil {
			t.Fatalf("invalid config %q exposed a title: %q", tc.data, *got)
		}
		if !tc.invalid && (got == nil || *got != tc.want) {
			t.Fatalf("config %q: expected explicit title %q, got %v", tc.data, tc.want, got)
		}
	}
}
