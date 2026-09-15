package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, path, data string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestUnifiedSettingsAreAuthoritativeAndIndependent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	write(t, filepath.Join(dir, "sounds.json"), `{"enabled":false}`)
	write(t, path, `{"ui":{"title":""},"background":42,"music":{"meters":false}}`)
	f, err := Load(path, false)
	if err != nil {
		t.Fatal(err)
	}
	var title struct {
		Title *string `json:"title"`
	}
	if err := f.Section("ui").Decode(&title); err != nil || title.Title == nil || *title.Title != "" {
		t.Fatal("empty title lost", err)
	}
	var sound struct {
		Enabled bool `json:"enabled"`
	}
	sound.Enabled = true
	if err := f.Section("sounds").Decode(&sound); err != nil || !sound.Enabled {
		t.Fatal("omitted section read legacy file", err)
	}
	var background struct {
		Image string `json:"image"`
	}
	if err := f.Section("background").Decode(&background); err == nil {
		t.Fatal("invalid section accepted")
	}
	write(t, path, `{"ui":{"title":"changed"}}`)
	if err := f.Section("ui").Decode(&title); err != nil || *title.Title != "" {
		t.Fatal("snapshot reread settings")
	}
	if f.Section("music").Path != path {
		t.Fatal("relative assets lost their source directory")
	}
}

func TestSettingsRejectInvalidDocumentButDeferSectionErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	for _, data := range []string{`null`, `[]`, `{}`, `{"ui":null}`, `{"typo":{}}`, `{} {}`, strings.Repeat(" ", maxFileBytes+1)} {
		write(t, path, data)
		_, err := Load(path, false)
		valid := data == `{}` || data == `{"ui":null}`
		if (err == nil) != valid {
			t.Fatalf("unexpected validity for %q: %v", data, err)
		}
	}
	write(t, path, `{"ui":{"title":"`+strings.Repeat("A", 4096)+`"},"sounds":{}}`)
	f, err := Load(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if f.Section("ui").Err == nil || f.Section("sounds").Err != nil {
		t.Fatal("size failure affected unrelated section")
	}
}

func TestLegacyMigrationPreservesFilesPathsAndExplicitValues(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	values := map[string]string{"ui": `{"title":""}`, "background": `{"image":"art/picture.png"}`, "sounds": `{"enabled":false,"volume":0}`, "music": `{"default":"Off","meters":false}`, "display": `{"interlaced":true}`, "input": `{"profiles":[]}`, "diagnostics": `{"path":"logs/events","enabled":true}`}
	for name, data := range values {
		write(t, filepath.Join(dir, name+".json"), data)
	}
	f, err := Load(path, false)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range values {
		if string(f.Section(name).Data) != data {
			t.Fatal("legacy settings were not read")
		}
	}
	if err := f.Migrate(); err != nil {
		t.Fatal(err)
	}
	migrated, err := Load(path, true)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range values {
		var want, got any
		_ = json.Unmarshal([]byte(data), &want)
		if name == "music" {
			want = map[string]any{"default_background": "Off", "show_audio_meters": false}
		}
		section := name
		if name == "ui" {
			want = map[string]any{"title": "", "navigation_sounds": map[string]any{"enabled": false, "volume": float64(0)}}
		}
		if name == "sounds" {
			section = "ui.navigation_sounds"
		}
		if err := migrated.Section(section).Decode(&got); err != nil {
			t.Fatal(err)
		}
		a, _ := json.Marshal(want)
		b, _ := json.Marshal(got)
		if string(a) != string(b) {
			t.Fatalf("lost %s", name)
		}
		old, err := os.ReadFile(filepath.Join(dir, name+".json"))
		if err != nil || string(old) != data {
			t.Fatal("legacy file modified")
		}
	}
	before, _ := os.ReadFile(path)
	var stored map[string]json.RawMessage
	if err := json.Unmarshal(before, &stored); err != nil {
		t.Fatal(err)
	}
	if stored["music_visuals"] == nil || stored["music"] != nil || stored["sounds"] != nil {
		t.Fatal("migration did not use the descriptive section name")
	}
	if err := f.Migrate(); err == nil {
		t.Fatal("migration overwrote newer settings")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("existing settings changed")
	}
}

func TestMissingExplicitSettingsAndBrokenMigration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	if _, err := Load(path, true); err == nil {
		t.Fatal("missing explicit settings accepted")
	}
	write(t, filepath.Join(dir, "ui.json"), `{"title":`)
	f, err := Load(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Migrate(); err == nil {
		t.Fatal("broken JSON migrated")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("failed migration left a settings file")
	}
}

func TestMigrationFormattingDoesNotInvalidateSection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	value := strings.Repeat("A", 4084)
	write(t, filepath.Join(dir, "ui.json"), `{"title":"`+value+`"}`)
	source, err := Load(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := source.Migrate(); err != nil {
		t.Fatal(err)
	}
	source, err = Load(path, true)
	if err != nil {
		t.Fatal(err)
	}
	var ui struct {
		Title string `json:"title"`
	}
	if err := source.Section("ui").Decode(&ui); err != nil || ui.Title != value {
		t.Fatal("migration whitespace invalidated section", err)
	}
}

func TestMusicVisualSectionAliasAndPrecedence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	for _, tc := range []struct{ data, want string }{
		{`{"music":{"meters":false}}`, `{"meters":false}`},
		{`{"music_visuals":{"show_audio_meters":false}}`, `{"show_audio_meters":false}`},
		{`{"music":{"meters":false},"music_visuals":{}}`, `{}`},
		{`{"music":{"meters":false},"music_visuals":null}`, `null`},
	} {
		write(t, path, tc.data)
		source, err := Load(path, true)
		if err != nil {
			t.Fatal(err)
		}
		if got := string(source.Section("music_visuals").Data); got != tc.want {
			t.Fatalf("section precedence: got %s, want %s", got, tc.want)
		}
	}
}

// TestNestedNavigationSounds verifies that current settings override the legacy
// section as a whole and that malformed title values do not change sound choices.
func TestNestedNavigationSounds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	for _, tc := range []struct {
		data, want string
		invalid    bool
	}{
		{`{"ui":{"navigation_sounds":{"volume":0}}}`, `{"volume":0}`, false},
		{`{"sounds":{"enabled":false}}`, `{"enabled":false}`, false},
		{`{"ui":{"title":42},"sounds":{"enabled":false}}`, `{"enabled":false}`, false},
		{`{"ui":{"navigation_sounds":{}},"sounds":{"enabled":false}}`, `{}`, false},
		{`{"ui":{"navigation_sounds":null},"sounds":{"enabled":true}}`, `null`, true},
		{`{"ui":null,"sounds":{"enabled":true}}`, ``, true},
		{`{"ui":{}}`, ``, false},
	} {
		write(t, path, tc.data)
		source, err := Load(path, true)
		if err != nil {
			t.Fatal(err)
		}
		section := source.Section("ui.navigation_sounds")
		var value map[string]any
		err = section.Decode(&value)
		if (err != nil) != tc.invalid || string(section.Data) != tc.want {
			t.Fatalf("%s: %+v, %v", tc.data, section, err)
		}
	}
}

func TestMigrationPreservesNestedNavigationChoice(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "ui.json"), `{"title":"","navigation_sounds":{"enabled":false,"volume":0}}`)
	write(t, filepath.Join(dir, "sounds.json"), `{"enabled":true,"volume":100}`)
	source, err := Load(filepath.Join(dir, "settings.json"), false)
	if err != nil {
		t.Fatal(err)
	}
	if err := source.Migrate(); err != nil {
		t.Fatal(err)
	}
	source, err = Load(source.Path, true)
	if err != nil {
		t.Fatal(err)
	}
	var sound struct {
		Enabled bool
		Volume  int
	}
	if err := source.Section("ui.navigation_sounds").Decode(&sound); err != nil {
		t.Fatal(err)
	}
	if sound.Enabled || sound.Volume != 0 {
		t.Fatal("legacy sounds replaced nested mute")
	}
}

func TestMigrationRejectsOversizedCombinedUIWithoutWriting(t *testing.T) {
	dir := t.TempDir()
	title := `{"title":"` + strings.Repeat("x", 4084) + `"}`
	write(t, filepath.Join(dir, "ui.json"), title)
	write(t, filepath.Join(dir, "sounds.json"), `{"enabled":false}`)
	source, err := Load(filepath.Join(dir, "settings.json"), false)
	if err != nil {
		t.Fatal(err)
	}
	if err := source.Migrate(); err == nil {
		t.Fatal("migration created oversized UI")
	}
	if _, err := os.Stat(source.Path); !os.IsNotExist(err) {
		t.Fatal("failed migration wrote settings")
	}
	original, err := os.ReadFile(filepath.Join(dir, "ui.json"))
	if err != nil || string(original) != title {
		t.Fatal("migration changed original UI")
	}
}
