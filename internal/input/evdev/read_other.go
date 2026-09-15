//go:build !linux

package evdev

import (
	"context"
	"errors"

	"misterfin-crt/internal/diagnostics"
	"misterfin-crt/internal/input/control"
)

// Read reports that native input is unavailable on this platform.
func Read(context.Context, Config, *diagnostics.Log) (<-chan control.Event, <-chan struct{}, error) {
	return nil, nil, errors.New("hardware input requires Linux")
}
