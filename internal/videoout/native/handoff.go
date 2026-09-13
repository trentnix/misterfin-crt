package native

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// framebufferHandoff protects loading frames until MPlayer presents video.
// Go locks only while drawing. MPlayer takes the same lock before its first
// frame and holds it until exit. Once claimed, Go only publishes overlays.
// Backend.mu serializes these methods. The lock file must keep its inode across
// decoder lifetimes, so closing this handle deliberately leaves the file intact.
type framebufferHandoff struct {
	file    *os.File
	claimed bool
}

// begin never waits for the decoder. A true result must be paired with end after
// presentation, including when the display returns an error.
func (h *framebufferHandoff) begin(path string) (bool, error) {
	if h.claimed {
		return false, nil
	}
	if h.file == nil {
		file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
		if err != nil {
			return false, fmt.Errorf("open framebuffer handoff: %w", err)
		}
		h.file = file
	}
	err := syscall.Flock(int(h.file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		h.claimed = true
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("lock framebuffer handoff: %w", err)
	}
	return true, nil
}

func (h *framebufferHandoff) end() {
	_ = syscall.Flock(int(h.file.Fd()), syscall.LOCK_UN)
}

func (h *framebufferHandoff) close() error {
	if h.file == nil {
		return nil
	}
	err := h.file.Close()
	h.file = nil
	return err
}
