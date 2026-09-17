package mister

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mistervision/internal/diagnostics"
)

func TestSettingsLogPreservesSectionsAndExcludesPrivateValues(t *testing.T) {
	data := settingsLog(t, strings.NewReader(`
ypbpr=0
password=secret-password
[MiSTer]
video_mode=640,480,60 ; secret comment
[Menu]
vsync_adjust=2
[MiSTerVisionInterlaced]
video_mode_ntsc=640,16,64,80,480,1,3,14,12.587
[private-core]
video_mode=42
[Menu]
video_mode=secret-value
`))
	for _, secret := range []string{"secret", "password", "private-core"} {
		if strings.Contains(data, secret) {
			t.Fatalf("log exposed %s", secret)
		}
	}
	var settings []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(data), "\n") {
		var e map[string]any
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatal(err)
		}
		if e["msg"] == "mister.setting" {
			settings = append(settings, e)
		}
	}
	if len(settings) != 4 || settings[0]["section"] != "top" || settings[1]["section"] != "mister" || settings[2]["section"] != "menu" || settings[3]["section"] != "mistervisioninterlaced" {
		t.Fatal(settings)
	}
	if !strings.Contains(data, `"rejected_values":1`) {
		t.Fatal(data)
	}
}

func TestSettingsReadAndEventCountsAreBounded(t *testing.T) {
	for _, source := range []string{strings.Repeat("ypbpr=0\n", 10000), strings.Repeat(";ignored\n", 20000)} {
		data := settingsLog(t, strings.NewReader(source))
		if !strings.Contains(data, `"limited":true`) {
			t.Fatal("missing truncation indication")
		}
		if strings.Count(data, `"msg":"mister.setting"`) > 64 {
			t.Fatal("unbounded settings inventory")
		}
	}
	RecordStartup(nil, false) // Disabled logging performs no hardware reads.
}

func TestNumericSettingsRejectUnrecognizedText(t *testing.T) {
	for _, value := range []string{"", "secret.invalid", "1\n2", "1=2", "+", strings.Repeat("1", 193)} {
		if numericSetting(value) {
			t.Fatalf("accepted %q", value)
		}
	}
	for _, value := range []string{"0", "-1", "640,16,64,80,480,1,3,14,12.587"} {
		if !numericSetting(value) {
			t.Fatalf("rejected %q", value)
		}
	}
}

// settingsLog exercises the real bounded writer with a synthetic INI source.
func settingsLog(t *testing.T, source io.Reader) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "debug.log")
	log, err := diagnostics.Open(diagnostics.Config{Enabled: true, Path: path, MaxBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	recordSettings(log, source)
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
