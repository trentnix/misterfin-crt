package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"misterfin-crt/internal/diagnostics"
	"misterfin-crt/internal/musicviz"
	"misterfin-crt/internal/sound"
)

// TestSettingsFallbacks checks real section loading through startup assembly,
// including the selected behavior, user notice, and drained diagnostic event.
func TestSettingsFallbacks(t *testing.T) {
	for _, tc := range []struct {
		name, section, data, kind, fallback string
		override                            bool
	}{
		{"title type", "ui", `{"title":42}`, "invalid", "default-title", false},
		{"background type", "background", `{"image":42}`, "invalid", "normal-artwork", false},
		{"background missing", "background", `{"image":"missing.png"}`, "not-found", "normal-artwork", false},
		{"background non-image", "background", `{"image":"private.txt"}`, "invalid", "normal-artwork", false},
		{"background corrupt image", "background", `{"image":"corrupt.png"}`, "invalid", "normal-artwork", false},
		{"sound range", "sounds", `{"volume":999}`, "invalid", "sounds-off", false},
		{"sound type", "sounds", `{"enabled":"yes"}`, "invalid", "sounds-off", false},
		{"sound unknown field", "sounds", `{"typo":true}`, "invalid", "sounds-off", false},
		{"sound null", "sounds", `null`, "invalid", "sounds-off", false},
		{"sound explicit missing", "sounds", `{}`, "not-found", "sounds-off", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			values := map[string]json.RawMessage{
				"ui":     json.RawMessage(`{"title":"Preserved title"}`),
				"sounds": json.RawMessage(`{"enabled":false,"volume":7}`),
			}
			if tc.section == "sounds" {
				values["ui"] = json.RawMessage(`{"title":"Preserved title","navigation_sounds":` + tc.data + `}`)
				delete(values, "sounds")
			} else {
				values[tc.section] = json.RawMessage(tc.data)
			}
			data, err := json.Marshal(values)
			if err != nil {
				t.Fatal(err)
			}
			for name, contents := range map[string][]byte{
				"settings.json": data,
				"private.txt":   []byte("private non-image contents"),
				"corrupt.png":   []byte("\x89PNG\r\n\x1a\ntruncated"),
			} {
				if err := os.WriteFile(filepath.Join(dir, name), contents, 0600); err != nil {
					t.Fatal(err)
				}
			}
			logPath := filepath.Join(dir, "events.log")
			log, err := diagnostics.Open(diagnostics.Config{Enabled: true, Path: logPath, MaxBytes: 65536})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { log.Close() })
			o := launchOptions{config: filepath.Join(dir, "jellyfin.conf"), stateDir: dir}
			if tc.override {
				o.soundConfig = filepath.Join(dir, "missing.json")
			}
			source := mustSettings(t, o)
			config, err := browserConfig(o, log, source)
			if err != nil {
				t.Fatal(err)
			}
			sounds, notice := browsingSounds(o, log, source)
			switch tc.section {
			case "ui":
				if config.Title != nil {
					t.Fatal("did not restore the default title")
				}
			case "background":
				if config.Background != nil {
					t.Fatal("did not restore normal artwork")
				}
			case "sounds":
				if sounds.Enabled || sounds.Volume != 0 || notice == "" {
					t.Fatal("did not mute invalid sounds with a notice")
				}
			}
			if tc.section != "ui" && (config.Title == nil || *config.Title != "Preserved title") {
				t.Fatal("fallback changed another section")
			}
			if tc.section != "sounds" && (sounds.Enabled || sounds.Volume != 7 || notice != "" || len(config.StartupNotices) != 1) {
				t.Fatal("fallback lost notice or changed sound settings")
			}
			if config.StateDir != dir || config.ConfigPath != o.config {
				t.Fatal("fallback changed browsing state")
			}
			if err := log.Close(); err != nil {
				t.Fatal(err)
			}
			data, err = os.ReadFile(logPath)
			if err != nil {
				t.Fatal(err)
			}
			events := decodeStartupEvents(t, data)
			if len(events) != 1 {
				t.Fatalf("expected exactly one fallback event, got %v", events)
			}
			e := events[0]
			if e["msg"] != "configuration.fallback" || e["configuration"] != map[string]string{"ui": "ui", "background": "background", "sounds": "ui.navigation_sounds"}[tc.section] || e["fallback"] != tc.fallback || e["error_kind"] != tc.kind {
				t.Fatal(e)
			}
			if strings.Contains(string(data), dir) || strings.Contains(string(data), "private") {
				t.Fatal("fallback log exposed configuration contents or paths")
			}
		})
	}
}

func TestBrokenUIRestoresTitleAndMutesNavigationSounds(t *testing.T) {
	for _, ui := range []string{
		`null`,
		`{"title":"` + strings.Repeat("x", 4096) + `"}`,
		`{"typo":true}`,
		`{"navigation_sound":{"enabled":false}}`,
		`{"title":"Custom","navigation_sounds":{"enabled":true},"typo":true}`,
	} {
		dir := t.TempDir()
		path := filepath.Join(dir, "settings.json")
		if err := os.WriteFile(path, []byte(`{"ui":`+ui+`,"sounds":{"enabled":true}}`), 0600); err != nil {
			t.Fatal(err)
		}
		logPath := filepath.Join(dir, "events.log")
		log, err := diagnostics.Open(diagnostics.Config{Enabled: true, Path: logPath, MaxBytes: 65536})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { log.Close() })
		o := launchOptions{config: filepath.Join(dir, "jellyfin.conf"), stateDir: dir}
		source := mustSettings(t, o)
		config, err := browserConfig(o, log, source)
		if err != nil {
			t.Fatal(err)
		}
		sounds, notice := browsingSounds(o, log, source)
		if config.Title != nil || len(config.StartupNotices) != 1 || sounds.Enabled || notice == "" {
			t.Fatal("invalid UI did not recover safely")
		}
		if err := log.Close(); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(logPath)
		if err != nil {
			t.Fatal(err)
		}
		events := decodeStartupEvents(t, data)
		if len(events) != 2 {
			t.Fatal("missing UI fallback events", events)
		}
		for i, want := range []struct{ section, fallback string }{{"ui", "default-title"}, {"ui.navigation_sounds", "sounds-off"}} {
			if events[i]["msg"] != "configuration.fallback" || events[i]["configuration"] != want.section || events[i]["fallback"] != want.fallback || events[i]["error_kind"] != "invalid" {
				t.Fatal(events)
			}
		}
	}
}

// TestMigrationPreservesEffectiveSettings compares consumer results, including
// failures and notices, rather than only checking the migrated JSON spelling.
func TestMigrationPreservesEffectiveSettings(t *testing.T) {
	for _, tc := range []struct {
		name  string
		files map[string]string
	}{
		{"defaults", nil},
		{"explicit zero values", map[string]string{"ui": `{"title":""}`, "sounds": `{"enabled":false,"volume":0}`, "music": `{"default":"Off","meters":false}`}},
		{"current names win", map[string]string{"ui": `{"title":" Custom ","navigation_sounds":{"enabled":false,"volume":0}}`, "sounds": `{"enabled":true,"volume":100}`, "music": `{"default":"Starfield","default_background":"Off","meters":true,"show_audio_meters":false}`}},
		{"null keeps defaults", map[string]string{"ui": `{"title":null}`, "music": `{"default":"Off","default_background":null,"meters":false,"show_audio_meters":null}`}},
		{"relative assets", map[string]string{"background": `{"image":"missing.png"}`, "music": `{"default":"Custom","backgrounds":[{"name":"Custom","type":"image","files":["art/custom.png"]}]}`}},
		{"independent recovery", map[string]string{"ui": `{"title":42}`, "sounds": `{"volume":999}`, "music": `{"backgrounds":[]}`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("MISTERFIN_MUSIC_CONFIG", "")
			for name, data := range tc.files {
				if err := os.WriteFile(filepath.Join(dir, name+".json"), []byte(data), 0600); err != nil {
					t.Fatal(err)
				}
			}
			o := launchOptions{config: filepath.Join(dir, "jellyfin.conf"), stateDir: dir}
			type effective struct {
				Title   *string
				Music   *musicviz.Config
				Sounds  sound.Config
				Notices []string
			}
			read := func() effective {
				source := mustSettings(t, o)
				config, err := browserConfig(o, nil, source)
				if err != nil {
					t.Fatal(err)
				}
				sounds, notice := browsingSounds(o, nil, source)
				result := effective{Title: config.Title, Sounds: sounds, Notices: config.StartupNotices}
				if config.MusicVisuals != nil {
					result.Music = &config.MusicVisuals.Config
				}
				if notice != "" {
					result.Notices = append(result.Notices, notice)
				}
				return result
			}
			before := read()
			if err := mustSettings(t, o).Migrate(); err != nil {
				t.Fatal(err)
			}
			after := read()
			if !reflect.DeepEqual(before, after) {
				t.Fatalf("effective settings changed: before=%+v after=%+v", before, after)
			}
		})
	}
}

func TestMusicSettingsFallbackAtStartup(t *testing.T) {
	for _, tc := range []struct{ data, override, kind string }{
		{`{"backgrounds":[]}`, "", "invalid"},
		{`null`, "", "invalid"},
		{`{}`, "missing.json", "not-found"},
	} {
		dir := t.TempDir()
		path := filepath.Join(dir, "settings.json")
		if err := os.WriteFile(path, []byte(`{"ui":{"title":"Kept"},"music_visuals":`+tc.data+`}`), 0600); err != nil {
			t.Fatal(err)
		}
		override := ""
		if tc.override != "" {
			override = filepath.Join(dir, tc.override)
		}
		t.Setenv("MISTERFIN_MUSIC_CONFIG", override)
		logPath := filepath.Join(dir, "events.log")
		log, err := diagnostics.Open(diagnostics.Config{Enabled: true, Path: logPath, MaxBytes: 65536})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { log.Close() })
		o := launchOptions{config: filepath.Join(dir, "jellyfin.conf"), stateDir: dir}
		config, err := browserConfig(o, log, mustSettings(t, o))
		if err != nil {
			t.Fatal(err)
		}
		if config.MusicVisuals != nil || config.Title == nil || *config.Title != "Kept" || len(config.StartupNotices) != 1 {
			t.Fatal("music failure changed unrelated browsing settings")
		}
		if err := log.Close(); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(logPath)
		if err != nil {
			t.Fatal(err)
		}
		events := decodeStartupEvents(t, data)
		if len(events) != 1 || events[0]["msg"] != "configuration.fallback" || events[0]["configuration"] != "music_visuals" || events[0]["fallback"] != "music-backgrounds-off" || events[0]["error_kind"] != tc.kind {
			t.Fatal(events)
		}
	}
}
