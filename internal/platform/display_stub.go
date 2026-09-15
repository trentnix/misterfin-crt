//go:build !linux || !cgo

package platform

import "errors"

// Open reports that native and headless framebuffer support requires Linux and cgo.
func Open(Options) (Display, error) {
	return nil, errors.New("framebuffer adapter requires Linux and CGO_ENABLED=1")
}
