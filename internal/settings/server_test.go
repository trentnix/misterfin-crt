package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServerDefaultsAndOverrides(t *testing.T) {
	if c, err := ParseServer(Section{}); err != nil || c != nil {
		t.Fatal("absent section must retain legacy configuration")
	}
	c, err := ParseServer(Section{Data: []byte(`{"url":" https://server/jellyfin/ "}`)})
	if err != nil || c.Provider != "jellyfin" || c.URL != "https://server/jellyfin" || c.InsecureTLS || c.Transcode != (Transcode{720, 576, 12000000}) {
		t.Fatalf("defaults: %+v %v", c, err)
	}
	c, err = ParseServer(Section{Data: []byte(`{"provider":"plex","url":"http://server:32400","insecure_tls":true,"transcode":{"max_width":640,"video_bitrate":8000000}}`)})
	if err != nil || !c.InsecureTLS || c.Transcode != (Transcode{640, 576, 8000000}) {
		t.Fatalf("override: %+v %v", c, err)
	}
}

func TestInvalidServerCannotFallbackOrExposeValues(t *testing.T) {
	for _, body := range []string{
		`null`, `{}`, `{"provider":"private-value","url":"http://server"}`, `{"url":"http://private-value@server"}`, `{"url":"https://server?token=private-value"}`,
		`{"url":"ftp://private-value"}`, `{"url":"https://server#private-value"}`, `{"url":"https://server","private-value":true}`,
		`{"url":"https://server","insecure_tls":"private-value"}`, `{"url":"https://server","transcode":{"max_width":0}}`,
		`{"url":"https://server","transcode":{"max_height":1081}}`, `{"url":"https://server","transcode":{"video_bitrate":50000001}}`,
		`{"url":"https://server","jellyfin":{"api_key":"private-value"}}`, `{"url":"https://server","jellyfin":{"username":"private-value"}}`,
		`{"provider":"plex","url":"https://server","jellyfin":{"api_key":"private-value","username":"someone"}}`,
	} {
		c, err := ParseServer(Section{Data: []byte(body)})
		if err == nil || c != nil || strings.Contains(err.Error(), "private-value") {
			t.Fatalf("invalid settings accepted or exposed: %v", err)
		}
	}
}

func TestMigrateServerPreservesExistingSettingsAndBackup(t *testing.T) {
	for _, diagnostics := range []string{`{"path":"custom.log"}`, `{"enabled":false,"path":"custom.log"}`} {
		t.Run(diagnostics, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "settings.json")
			original := `{"ui":{"title":"","navigation_sounds":{"enabled":false}},"display":{"interlaced":true},"background":{"image":"art/file.png"},"diagnostics":` + diagnostics + `}`
			write(t, path, original)
			source, err := Load(path, true)
			if err != nil {
				t.Fatal(err)
			}
			server, err := ParseServer(Section{Data: []byte(`{"url":"https://server/jellyfin","insecure_tls":true,"transcode":{"max_width":640},"jellyfin":{"api_key":"private-value","username":"viewer"}}`)})
			if err != nil {
				t.Fatal(err)
			}
			if err := source.MigrateServer(*server, true); err != nil {
				t.Fatal(err)
			}
			backup, err := os.ReadFile(path + ".before-server")
			if err != nil || string(backup) != original {
				t.Fatal("original document not preserved")
			}
			for _, p := range []string{path, path + ".before-server"} {
				info, err := os.Stat(p)
				if err != nil || info.Mode().Perm() != 0600 {
					t.Fatal("migration file is not private")
				}
			}
			next, err := Load(path, true)
			if err != nil {
				t.Fatal(err)
			}
			got, err := ParseServer(next.Section("server"))
			if err != nil || got.Jellyfin.APIKey != "private-value" || got.Transcode.MaxWidth != 640 {
				t.Fatal("connection changed")
			}
			for _, name := range []string{"ui", "display", "background"} {
				var a, b any
				if err := source.Section(name).Decode(&a); err != nil {
					t.Fatal(err)
				}
				if err := next.Section(name).Decode(&b); err != nil {
					t.Fatal(err)
				}
				before, _ := json.Marshal(a)
				after, _ := json.Marshal(b)
				if string(before) != string(after) {
					t.Fatalf("changed %s", name)
				}
			}
			var logging struct {
				Enabled bool
				Path    string
			}
			if err := next.Section("diagnostics").Decode(&logging); err != nil {
				t.Fatal(err)
			}
			if logging.Enabled == strings.Contains(diagnostics, `"enabled":false`) || logging.Path != "custom.log" {
				t.Fatal("diagnostics precedence changed")
			}
			before, _ := os.ReadFile(path)
			if err := next.MigrateServer(*server, true); err == nil {
				t.Fatal("overwrote existing server")
			}
			after, _ := os.ReadFile(path)
			if string(before) != string(after) {
				t.Fatal("repeated migration changed settings")
			}
		})
	}
}

func TestMigrateServerRefusesStaleDocumentAndExistingBackup(t *testing.T) {
	server, _ := ParseServer(Section{Data: []byte(`{"url":"http://server"}`)})
	for _, change := range []string{"edit", "backup"} {
		t.Run(change, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "settings.json")
			write(t, path, `{}`)
			source, err := Load(path, true)
			if err != nil {
				t.Fatal(err)
			}
			if change == "edit" {
				write(t, path, `{"ui":{"title":"New"}}`)
			} else {
				write(t, path+".before-server", "preserve")
			}
			before, _ := os.ReadFile(path)
			if err := source.MigrateServer(*server, false); err == nil {
				t.Fatal("unsafe migration succeeded")
			}
			after, _ := os.ReadFile(path)
			if string(before) != string(after) {
				t.Fatal("existing settings replaced")
			}
		})
	}
}

func TestOversizedMigratedConnectionLeavesFilesAlone(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	write(t, path, `{}`)
	source, err := Load(path, true)
	if err != nil {
		t.Fatal(err)
	}
	server, _ := ParseServer(Section{Data: []byte(`{"url":"http://server"}`)})
	server.Jellyfin = &JellyfinLogin{APIKey: strings.Repeat("private", 1000), Username: "viewer"}
	if err := source.MigrateServer(*server, false); err == nil || strings.Contains(err.Error(), "private") {
		t.Fatalf("unsafe oversized migration: %v", err)
	}
	after, _ := os.ReadFile(path)
	if string(after) != `{}` {
		t.Fatal("failed migration changed original")
	}
	if _, err := os.Stat(path + ".before-server"); !os.IsNotExist(err) {
		t.Fatal("invalid connection created a backup")
	}
}

func TestMigratingDebugLogCannotOverflowDiagnostics(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	original := `{"diagnostics":{"path":"` + strings.Repeat("x", 4085) + `"}}`
	write(t, path, original)
	source, err := Load(path, true)
	if err != nil {
		t.Fatal(err)
	}
	server, _ := ParseServer(Section{Data: []byte(`{"url":"http://server"}`)})
	if err := source.MigrateServer(*server, true); err == nil {
		t.Fatal("migration created oversized diagnostics")
	}
	after, _ := os.ReadFile(path)
	if string(after) != original {
		t.Fatal("failed migration changed original")
	}
}
