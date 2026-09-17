package displaymode

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"misterfin-crt/internal/update"
)

func TestChildRestartStatus(t *testing.T) {
	for _, status := range []int{0, 1, update.RestartExitCode} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			err := exec.Command("sh", "-c", "exit "+strconv.Itoa(status)).Run()
			got := childResult(err)
			if status == update.RestartExitCode {
				if !update.RestartRequested(got) {
					t.Fatalf("restart status lost: %v", got)
				}
				if update.RestartRequested(errors.Join(got, errors.New("core restoration failed"))) {
					t.Fatal("restart permitted after failed core restoration")
				}
			} else if got != err {
				t.Fatalf("ordinary exit changed: %v -> %v", err, got)
			}
		})
	}
}

// Main creates the marker only after the core load. Missing, empty, and zero
// values must not let an activation key race ahead of its menu input routing.
func TestWaitMenuReady(t *testing.T) {
	for _, initial := range []string{"missing", "", "0"} {
		t.Run(initial, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "OSD_VISIBLE")
			if initial != "missing" {
				if err := os.WriteFile(path, []byte(initial), 0600); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- waitMenuReady(ctx, path) }()
			select {
			case err := <-done:
				t.Fatalf("menu was not ready: %v", err)
			case <-time.After(20 * time.Millisecond):
			}
			if err := os.WriteFile(path, []byte("1\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestWaitMenuReadyFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if err := waitMenuReady(ctx, filepath.Join(t.TempDir(), "missing")); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("missing acknowledgment: %v", err)
	}
	if err := waitMenuReady(t.Context(), t.TempDir()); err == nil {
		t.Fatal("unreadable marker accepted")
	}
	// A canceled startup must not continue even if a ready marker exists.
	path := filepath.Join(t.TempDir(), "OSD_VISIBLE")
	if err := os.WriteFile(path, []byte("1"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := waitMenuReady(ctx, path); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("canceled startup continued: %v", err)
	}
}
