// Package input provides terminal events and controller configuration.
package input

import (
	"context"

	"mistervision/internal/input/control"
	"mistervision/internal/terminal"
)

// ReadTerminal returns semantic actions with keyboard labels, followed by a
// completion channel. On Linux it reads the controlling terminal, falling back
// to terminal stdin when there is no controlling terminal. It disables line
// buffering and echo while preserving signal keys. Platforms without terminal
// input support return an error.
//
// The caller must cancel ctx and wait for completion before opening another
// reader. Completion means terminal settings have been restored and both
// channels are closed. An error means no reader was started and both returned
// channels are nil. Published binding labels must not be modified.
func ReadTerminal(ctx context.Context) (<-chan control.Event, <-chan struct{}, error) {
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
