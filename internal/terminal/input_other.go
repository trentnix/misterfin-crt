//go:build !linux

package terminal

import (
	"context"
	"errors"

	"misterfin-crt/internal/input/control"
)

// Read reports that terminal input currently requires Linux. No reader is started.
func Read(context.Context) (<-chan control.Action, <-chan struct{}, error) {
	return nil, nil, errors.New("desktop input currently requires Linux")
}
