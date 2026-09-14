//go:build linux && cgo

package alsa

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNativeAdapterUsesNonblockingPCM(t *testing.T) {
	path := filepath.Join(t.TempDir(), "asound.conf")
	if err := os.WriteFile(path, []byte("pcm.!default { type null }\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ALSA_CONFIG_PATH", path)
	s, err := Open()
	if err != nil {
		t.Skipf("ALSA null plugin unavailable: %v", err)
	}
	defer s.Close()
	samples := make([]int16, 1024)
	samples[0], samples[1] = 100, -100
	n, err := s.Write(samples)
	if err != nil || n < 0 || n > 512 {
		t.Fatalf("write: %d %v", n, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}
