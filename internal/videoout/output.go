// Package videoout presents shared UX frames through the selected output backend.
package videoout

import (
	"time"

	"misterfin-crt/internal/platform"
)

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
// goroutine and bracket its lifetime. Each backend coordinates the actual
// display handoff, which may occur later when the first video frame is ready.
// Close releases backend resources. The caller owns the underlying Display.
type Output interface {
	// Geometry returns logical UI and physical output dimensions.
	Geometry() platform.Geometry
	// FrameInterval supplies the positive presentation interval for the current
	// mode. Outputs that composite video must sample faster than overlay-only
	// outputs. This cadence does not change the decoder's playback clock.
	FrameInterval(video bool) time.Duration
	// Present consumes borrowed pixels synchronously. Implementations must not
	// retain the slices after returning unless they copy them.
	Present(Frame) error
	// Acquire registers a decoder that may begin drawing. Each acquisition
	// must have a matching Release, including during canceled playback.
	Acquire()
	// Release relinquishes one decoder ownership claim after it stops drawing.
	Release()
	// Clear discards stale playback output before a new item and after playback.
	Clear()
	// Close releases backend resources without closing the underlying display.
	// The caller must stop decoder activity before closing the backend.
	Close() error
}

// FrameNotifier optionally wakes the UI loop when an output's video source
// changes. Timer-driven drawing still animates overlays. Notifications coalesce
// when drawing falls behind, so presentation always uses the latest frame.
type FrameNotifier interface {
	FrameUpdates() (<-chan struct{}, error)
}
