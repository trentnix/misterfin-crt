// Package input selects the keyboard or hardware event source at startup.
package input

import (
	"context"
	"misterfin-go/internal/input/evdev"
	"misterfin-go/internal/terminal"
)

// Read keeps terminal translations out of the hardware controller path.
func Read(ctx context.Context, headless bool) (<-chan string, <-chan struct{}, error) {
	if headless {
		return terminal.Read(ctx)
	}
	return evdev.Read(ctx)
}
