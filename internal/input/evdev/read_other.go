//go:build !linux

package evdev

import (
	"context"
	"errors"
	"misterfin-crt/internal/input/control"
)

func Read(context.Context, Config) (<-chan control.Event, <-chan struct{}, error) {
	return nil, nil, errors.New("hardware input requires Linux")
}
