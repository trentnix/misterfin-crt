package diagnostics

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDisabledDoesNotCreateFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "debug.log")
	log, err := Open(Config{Path: path})
	if err != nil || log != nil {
		t.Fatalf("%v %v", log, err)
	}
	log.Record("disabled")
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Fatal("disabled logging touched disk")
	}
}

func TestRotationRetainsOnlyTwoBoundedFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "debug.log")
	w := &rotatingFile{path: path, limit: 4096}
	if err := w.open(); err != nil {
		t.Fatal(err)
	}
	record := []byte(strings.Repeat("x", 1023) + "\n")
	for i := 0; i < 13; i++ {
		if _, err := w.Write(record); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.file.Close(); err != nil {
		t.Fatal(err)
	}
	for name, size := range map[string]int64{path: 1024, path + ".1": 4096} {
		info, err := os.Stat(name)
		if err != nil {
			t.Fatal(err)
		}
		if info.Size() != size {
			t.Fatalf("%s: %v", name, info)
		}
	}
	files, _ := os.ReadDir(filepath.Dir(path))
	if len(files) != 2 {
		t.Fatal(files)
	}
	log, err := Open(Config{Enabled: true, Path: path, MaxBytes: 4096})
	if err != nil {
		t.Fatal(err)
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".1"); !os.IsNotExist(err) {
		t.Fatal("stale rotation survived new launch")
	}
}

func TestLogPermissionsWhereSupported(t *testing.T) {
	dir := t.TempDir()
	probe, err := os.CreateTemp(dir, "permissions-")
	if err != nil {
		t.Fatal(err)
	}
	info, err := probe.Stat()
	probe.Close()
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Skip("filesystem does not enforce Unix owner-only permissions")
	}
	path := filepath.Join(dir, "debug.log")
	if err := os.WriteFile(path, []byte("old log"), 0644); err != nil {
		t.Fatal(err)
	}
	l, err := Open(Config{Enabled: true, Path: path, MaxBytes: 4096})
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	info, err = os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatal("existing log did not become owner-only")
	}
}

func TestFullQueueDoesNotWaitAndReportsDrops(t *testing.T) {
	// Hold the worker until producers finish. No filesystem speed assumptions.
	l := &Log{queue: make(chan slog.Record, 1), done: make(chan struct{})}
	finished := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			l.Record("event")
		}
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(3 * time.Second):
		t.Fatal("full log queue blocked caller")
	}
	if l.dropped.Load() != 999 {
		t.Fatal(l.dropped.Load())
	}
	path := filepath.Join(t.TempDir(), "debug.log")
	w := &rotatingFile{path: path, limit: 4096}
	if err := w.open(); err != nil {
		t.Fatal(err)
	}
	go l.write(w)
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), `"msg":"diagnostics.dropped"`) || !strings.Contains(string(b), `"events":999`) {
		t.Fatal(string(b))
	}
}

func TestConcurrentCloseAndLateEvents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "debug.log")
	l, err := Open(Config{Enabled: true, Path: path, MaxBytes: 4096})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Go(func() {
			for n := 0; n < 1000; n++ {
				l.Record("event", slog.Int("n", n))
			}
		})
	}
	wg.Go(func() {
		if err := l.Close(); err != nil {
			t.Error(err)
		}
	})
	wg.Wait()
	l.Record("after-close")
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if line != "" && !json.Valid([]byte(line)) {
			t.Fatal(line)
		}
	}
}

func TestWriteFailureDisablesLogging(t *testing.T) {
	path := filepath.Join(t.TempDir(), "debug.log")
	l, err := Open(Config{Enabled: true, Path: path, MaxBytes: 4096})
	if err != nil {
		t.Fatal(err)
	}
	l.Record("oversize", slog.String("value", strings.Repeat("x", 5000)))
	if l.Close() == nil || !l.failed.Load() {
		t.Fatal("write failure not reported")
	}
	info, _ := os.Stat(path)
	if info.Size() > 4096 {
		t.Fatal("size bound exceeded")
	}
	l.Record("ignored")
}

func TestRequestExcludesCredentialsAndUntrustedMethod(t *testing.T) {
	path := filepath.Join(t.TempDir(), "debug.log")
	l, err := Open(Config{Enabled: true, Path: path, MaxBytes: 4096})
	if err != nil {
		t.Fatal(err)
	}
	l.Request("GET", "https://user:password@secret-host/QuickConnect/Connect?secret=quick-secret&ApiKey=token#fragment", 200, 15*time.Millisecond, 42, false)
	l.Request("secret-method", "/Items?ApiKey=second-secret", 503, time.Second, 0, true)
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	for _, secret := range []string{"user", "password", "secret-host", "quick-secret", "token", "fragment", "secret-method", "second-secret"} {
		if strings.Contains(string(b), secret) {
			t.Fatalf("leaked %s", secret)
		}
	}
	if !strings.Contains(string(b), `"path":"/QuickConnect/Connect"`) || !strings.Contains(string(b), `"elapsed_ms":15`) {
		t.Fatal(string(b))
	}
}
