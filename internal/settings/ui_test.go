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

func TestUICarouselOptions(t *testing.T) {
	for _, tc := range []struct {
		name, data                       string
		collections, playlists           bool
		collectionsError, playlistsError bool
	}{
		{"omitted", `{}`, true, true, false, false},
		{"enabled", `{"show_collections":true,"show_playlists":true}`, true, true, false, false},
		{"disabled", `{"show_collections":false,"show_playlists":false}`, false, false, false, false},
		{"independent", `{"show_collections":false}`, false, true, false, false},
		{"wrong collections type", `{"show_collections":"no","show_playlists":false}`, true, false, true, false},
		{"wrong playlists type", `{"show_collections":false,"show_playlists":0}`, false, true, false, true},
		{"null", `{"show_collections":null,"show_playlists":null}`, true, true, true, true},
		{"invalid title", `{"title":42,"show_collections":false,"show_playlists":false}`, false, false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ui := ParseUI(Section{Data: []byte(tc.data)})
			collections := ui.ShowCollections == nil || *ui.ShowCollections
			playlists := ui.ShowPlaylists == nil || *ui.ShowPlaylists
			if collections != tc.collections || playlists != tc.playlists || (ui.ShowCollectionsError != nil) != tc.collectionsError || (ui.ShowPlaylistsError != nil) != tc.playlistsError {
				t.Fatalf("unexpected options: %+v", ui)
			}
		})
	}
}
