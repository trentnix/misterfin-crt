//go:build !linux

package evdev

import (
	"context"
	"errors"
)

func Read(context.Context, Config) (<-chan string, <-chan struct{}, error) {
	return nil, nil, errors.New("hardware input requires Linux")
}
