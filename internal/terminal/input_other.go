//go:build !linux

package terminal

import (
	"context"
	"errors"
)

func Read(context.Context) (<-chan string, <-chan struct{}, error) {
	return nil, nil, errors.New("desktop input currently requires Linux")
}
