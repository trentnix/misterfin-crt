package main

import (
	"encoding/json"
	"errors"
	"misterfin-crt/internal/update"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStartupFailureBeforeBrowserIsRecorded(t *testing.T) {
	for _, tc := range []struct {
		name, stage string
		args        []string
		display     string
	}{
		{"framebuffer", "display-open", []string{"-headless=invalid"}, ""},
		{"display configuration", "display-config", nil, `{"secret-setting":"private-value"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			config := filepath.Join(dir, "jellyfin.conf")
			// DEBUGLOG must still work after a malformed transcode profile.
			if err := os.WriteFile(config, []byte("640xbroken\nDEBUGLOG\nhttp://secret.invalid\nsecret-token\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if tc.display != "" {
				if err := os.WriteFile(filepath.Join(dir, "display.json"), []byte(tc.display), 0600); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("MISTERFIN_SETTINGS", "")
			t.Setenv("MISTERFIN_FB", "")
			t.Setenv("MISTERFIN_FRAME_OUT", "")
			t.Setenv("MISTERFIN_CRT_INTERLACED", "")
			args := os.Args
			t.Cleanup(func() { os.Args = args })
			os.Args = append([]string{"misterfin-crt", "-browse", "-config=" + config}, tc.args...)
			if err := run(); err == nil {
				t.Fatal("expected startup failure")
			}
			data, err := os.ReadFile(filepath.Join(dir, "debug.log"))
			if err != nil {
				t.Fatal(err)
			}
			events := decodeStartupEvents(t, data)
			if events[0]["msg"] != "application.start" {
				t.Fatal(events)
			}
			failure, exit := events[len(events)-2], events[len(events)-1]
			if failure["msg"] != "application.failure" || failure["stage"] != tc.stage || exit["failed"] != true {
				t.Fatal(events)
			}
			for _, secret := range []string{"secret", "private-value", dir, "640xbroken"} {
				if strings.Contains(string(data), secret) {
					t.Fatalf("log exposed %q", secret)
				}
			}
		})
	}
}

func TestSupervisorAndApplicationLogsRemainIndependent(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "jellyfin.conf")
	if err := os.WriteFile(config, []byte("DEBUGLOG\n"), 0600); err != nil {
		t.Fatal(err)
	}
	o := launchOptions{browse: true, config: config}
	parent, err := openStartupDiagnostics(o, true, mustSettings(t, o))
	if err != nil {
		t.Fatal(err)
	}
	parent.phase("interlaced-supervisor")
	child, err := openStartupDiagnostics(o, false, mustSettings(t, o))
	if err != nil {
		t.Fatal(err)
	}
	child.phase("browser")
	child.close(nil)
	parent.close(errors.New("private core path"))
	for _, tc := range []struct {
		suffix, role string
		failed       bool
	}{
		{"", "application", false}, {".supervisor", "display-supervisor", true},
	} {
		data, err := os.ReadFile(filepath.Join(dir, "debug.log"+tc.suffix))
		if err != nil {
			t.Fatal(err)
		}
		events := decodeStartupEvents(t, data)
		if events[0]["role"] != tc.role || events[len(events)-1]["failed"] != tc.failed {
			t.Fatal(events)
		}
		if strings.Contains(string(data), "private core path") {
			t.Fatal("raw error leaked")
		}
	}
}

func TestDisabledStartupDiagnosticsCreateNoLogs(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "jellyfin.conf"), []byte("DEBUGLOG\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "diagnostics.json"), []byte(`{"enabled":false,"path":"logs/debug.log"}`), 0600); err != nil {
		t.Fatal(err)
	}
	trace, err := openStartupDiagnostics(launchOptions{browse: true, config: filepath.Join(dir, "jellyfin.conf")}, true, mustSettings(t, launchOptions{browse: true, config: filepath.Join(dir, "jellyfin.conf")}))
	if err != nil {
		t.Fatal(err)
	}
	trace.phase("display-open")
	trace.close(errors.New("unavailable"))
	if trace.log != nil {
		t.Fatal("explicit disable ignored")
	}
	if _, err := os.Stat(filepath.Join(dir, "logs")); !os.IsNotExist(err) {
		t.Fatal("disabled logging created directory")
	}
}

// decodeStartupEvents reads complete JSON lines after shutdown has drained them.
func decodeStartupEvents(t *testing.T, data []byte) []map[string]any {
	t.Helper()
	var events []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatal(err)
		}
		events = append(events, event)
	}
	return events
}

func TestBadDiagnosticsDoNotPreventStartup(t *testing.T) {
	for _, data := range []string{`{"enabled":true,"max_bytes":1}`, `{"enabled":true,"path":"blocked/log"}`, `null`} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{"diagnostics":`+data+`}`), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "blocked"), []byte("file"), 0600); err != nil {
			t.Fatal(err)
		}
		// The logger cannot record its own startup failure. Capture the
		// fallback warning at stderr and verify the same notice reaches the UI.
		stderr, err := os.CreateTemp(t.TempDir(), "stderr")
		if err != nil {
			t.Fatal(err)
		}
		original := os.Stderr
		trace, err := func() (*startupDiagnostics, error) {
			os.Stderr = stderr
			defer func() { os.Stderr = original }()
			return openStartupDiagnostics(launchOptions{browse: true, config: filepath.Join(dir, "jellyfin.conf")}, false, mustSettings(t, launchOptions{browse: true, config: filepath.Join(dir, "jellyfin.conf")}))
		}()
		if closeErr := stderr.Close(); closeErr != nil {
			t.Fatal(closeErr)
		}
		if err != nil {
			t.Fatal(err)
		}
		if trace.log != nil || trace.notice == "" {
			t.Fatal("failed logger needs a notice and disabled log")
		}
		warning, readErr := os.ReadFile(stderr.Name())
		if readErr != nil {
			t.Fatal(readErr)
		}
		if string(warning) != trace.notice+"\n" {
			t.Fatalf("stderr did not report the fallback: %q", warning)
		}
		trace.phase("browser")
		trace.close(nil)
		if strings.Contains(trace.notice, dir) {
			t.Fatal("raw path exposed in warning")
		}
	}
}

func TestRestartDiagnosticsDistinguishCleanupFailure(t *testing.T) {
	for _, failure := range []bool{false, true} {
		dir := t.TempDir()
		config := filepath.Join(dir, "jellyfin.conf")
		if err := os.WriteFile(config, []byte("DEBUGLOG\n"), 0600); err != nil {
			t.Fatal(err)
		}
		o := launchOptions{browse: true, config: config}
		trace, err := openStartupDiagnostics(o, false, mustSettings(t, o))
		if err != nil {
			t.Fatal(err)
		}
		result := update.ErrRestart
		if failure {
			result = errors.Join(result, errors.New("cleanup failed"))
		}
		trace.close(result)
		data, err := os.ReadFile(filepath.Join(dir, "debug.log"))
		if err != nil {
			t.Fatal(err)
		}
		events := decodeStartupEvents(t, data)
		if events[len(events)-1]["failed"] != failure {
			t.Fatal(events)
		}
		if strings.Contains(string(data), "update.restart") == failure {
			t.Fatal(events)
		}
	}
}
