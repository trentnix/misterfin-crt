// Package input selects the keyboard or hardware event source at startup.
package input

import (
	"context"

	"misterfin-go/internal/input/control"
	"misterfin-go/internal/input/evdev"
	"misterfin-go/internal/terminal"
)

// Read selects terminal keys for headless output and direct evdev events for
// MiSTer. It returns events with binding labels and a completion channel.
// The caller must cancel ctx and wait for completion before releasing input
// resources. Channels close when the reader exits. An error means no reader was started.
func Read(ctx context.Context, headless bool, config evdev.Config) (<-chan control.Event, <-chan struct{}, error) {
	if headless {
		keys, stopped, err := terminal.Read(ctx)
		if err != nil {
			return nil, nil, err
		}
		out, done := make(chan control.Event, 32), make(chan struct{})
		go func() {
			defer close(done)
			defer close(out)
			defer func() { <-stopped }()
			for {
				select {
				case <-ctx.Done():
					return
				case key, ok := <-keys:
					if !ok {
						return
					}
					select {
					case out <- control.Event{Action: key, Labels: control.KeyboardLabels()}:
					case <-ctx.Done():
						return
					}
				}
			}
		}()
		return out, done, nil
	}
	return evdev.Read(ctx, config)
}
