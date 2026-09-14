//go:build !linux || !cgo

// Package alsa supplies optional Linux UI audio, including MiSTer's ALSA sink.
package alsa

import (
	"errors"
	"misterfin-crt/internal/sound"
)

// Open reports unavailable audio on builds without the native ALSA adapter.
func Open() (sound.Stream, error) { return nil, errors.New("UI audio requires Linux and cgo") }
