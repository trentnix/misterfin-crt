package diagnostics

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigurationFallbackRecordsOnlySafeFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log")
	log, err := Open(Config{Enabled: true, Path: path, MaxBytes: 65536})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		err  error
		kind string
	}{
		{fmt.Errorf("private config value: %w", os.ErrNotExist), "not-found"},
		{&os.PathError{Op: "read", Path: "private-path", Err: os.ErrPermission}, "permission"},
		{&os.PathError{Op: "read", Path: "private-path", Err: errors.New("private disk error")}, "file-io"},
		{errors.New("private configuration contents"), "invalid"},
	} {
		log.ConfigurationFallback("ui.json", "default-title", tc.err)
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "private") {
		t.Fatal("fallback exposed raw error or settings")
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	wantKinds := []string{"not-found", "permission", "file-io", "invalid"}
	if len(lines) != len(wantKinds) {
		t.Fatal("missing fallback events")
	}
	for i, line := range lines {
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatal(err)
		}
		if event["msg"] != "configuration.fallback" || event["configuration"] != "ui.json" || event["fallback"] != "default-title" || event["error_kind"] != wantKinds[i] {
			t.Fatal(event)
		}
		if len(event) != 6 {
			t.Fatalf("unexpected fields: %v", event)
		}
	}
	var disabled *Log
	disabled.ConfigurationFallback("ui.json", "default-title", errors.New("private"))
}
