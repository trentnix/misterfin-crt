// Package input selects the keyboard or hardware event source at startup.
package input

import (
	"context"
	"misterfin-go/internal/input/evdev"
	"misterfin-go/internal/terminal"
)

// Read selects terminal keys for headless output and direct evdev events for
// MiSTer. It returns an action channel and a completion channel. The caller must
// cancel ctx and wait for completion before releasing input resources. Channels
// close when the reader exits. An error means no reader was started.
func Read(ctx context.Context, headless bool) (<-chan string, <-chan struct{}, error) {
	if headless {
		return terminal.Read(ctx)
	}
	return evdev.Read(ctx)
}
