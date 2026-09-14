//go:build linux && cgo

// Package alsa supplies optional Linux UI audio, including MiSTer's ALSA sink.
package alsa

/*
#cgo CFLAGS: -std=c11 -Wall -Wextra
#cgo LDFLAGS: -ldl
#include "adapter.h"
*/
import "C"

import (
	"errors"
	"unsafe"

	"misterfin-crt/internal/sound"
)

// stream owns one nonblocking ALSA handle. The sound worker serializes access.
type stream struct{ handle *C.mf_sound }

// Open borrows the default ALSA device. Libraries are loaded at runtime so a
// missing ALSA installation does not prevent the application from starting.
func Open() (sound.Stream, error) {
	h := C.mf_sound_open()
	if h == nil {
		return nil, errors.New("UI audio device unavailable")
	}
	return &stream{handle: h}, nil
}

// Write copies stereo frames into ALSA without waiting for available capacity.
func (s *stream) Write(samples []int16) (int, error) {
	if len(samples) == 0 {
		return 0, nil
	}
	n := C.mf_sound_write(s.handle, (*C.int16_t)(unsafe.Pointer(&samples[0])), C.ulong(len(samples)/2))
	if n < 0 {
		return 0, errors.New("UI audio write failed")
	}
	return int(n), nil
}

// Close discards buffered sound and releases the device. Repeated calls are safe.
func (s *stream) Close() error {
	if s.handle != nil {
		C.mf_sound_close(s.handle)
		s.handle = nil
	}
	return nil
}
