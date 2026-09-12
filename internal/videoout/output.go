// Package videoout presents shared UX frames through the selected output backend.
package videoout

import "misterfin-go/internal/platform"

// Frame describes what the UX wants to display, without choosing an output path.
// UI is a full BGRX browser frame. During video playback, Overlay contains
// straight-alpha BGRA pixels and UI serves as the companion player's backdrop.
// Video remains true while loading or seeking, even when no decoder owns output.
// Present borrows the slices for the duration of the call.
type Frame struct {
	UI      []byte
	Overlay []byte
	Video   bool
}

// Output is the browser's only display boundary for both browsing and playback.
// Call Present from the UI loop. Acquire and Release may come from the decoder
// goroutine and mark when an external player owns the physical display.
// Close releases backend resources. The caller owns the underlying Display.
type Output interface {
	Geometry() platform.Geometry
	Present(Frame) error
	Acquire()
	Release()
	// Clear discards stale playback output before a new item and after playback.
	Clear()
	Close() error
}
