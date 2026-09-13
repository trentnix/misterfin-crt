package framefile

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFrameWatchFollowsAtomicReplacements(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "video")
	w, err := watchFrames(path)
	if err != nil {
		t.Fatal(err)
	}
	defer w.close()
	for i := 0; i < 3; i++ {
		temporary := filepath.Join(dir, "next")
		if err := os.WriteFile(temporary, []byte{byte(i)}, 0600); err != nil {
			t.Fatal(err)
		}
		select {
		case <-w.updates:
			t.Fatal("temporary frame caused a premature notification")
		case <-time.After(10 * time.Millisecond):
		}
		if err := os.Rename(temporary, path); err != nil {
			t.Fatal(err)
		}
		select {
		case <-w.updates:
			data, err := os.ReadFile(path)
			if err != nil || len(data) != 1 || data[0] != byte(i) {
				t.Fatalf("notification preceded the complete frame: %v, %v", data, err)
			}
		case <-time.After(time.Second):
			t.Fatal("complete frame did not wake presentation")
		}
	}
}

func TestFrameWatchCloseInterruptsIdleRead(t *testing.T) {
	w, err := watchFrames(filepath.Join(t.TempDir(), "video"))
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- w.close() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("closing the watcher left its reader blocked")
	}
	if err := w.close(); err != nil {
		t.Fatalf("close is not idempotent: %v", err)
	}
}
