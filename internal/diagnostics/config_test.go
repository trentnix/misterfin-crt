package diagnostics

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigurationDefaultsAndLegacyOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "diagnostics.json")
	for _, legacy := range []bool{false, true} {
		cfg, err := LoadConfig(path, legacy)
		if err != nil || cfg.Enabled != legacy || cfg.MaxBytes != 1<<20 || cfg.Path != filepath.Join(filepath.Dir(path), "debug.log") {
			t.Fatalf("%+v %v", cfg, err)
		}
	}
	if err := os.WriteFile(path, []byte(`{"enabled":false,"path":"logs/session.log","max_bytes":8192}`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path, true)
	if err != nil || cfg.Enabled || cfg.Path != filepath.Join(filepath.Dir(path), "logs/session.log") || cfg.MaxBytes != 8192 {
		t.Fatalf("%+v %v", cfg, err)
	}
}

func TestInvalidConfiguration(t *testing.T) {
	for _, data := range []string{"", "null", "[]", `{"max_bytes":0}`, `{"max_bytes":67108865}`, `{"path":""}`, `{"unknown":true}`, `{} {}`, `{"enabled":"yes"}`} {
		path := filepath.Join(t.TempDir(), "diagnostics.json")
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadConfig(path, false); err == nil {
			t.Errorf("accepted %q", data)
		}
	}
}
