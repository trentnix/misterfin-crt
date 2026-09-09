//go:build !linux || !cgo

package platform

import "errors"

func Open(Options) (Display, error) {
	return nil, errors.New("framebuffer adapter requires Linux and CGO_ENABLED=1")
}
