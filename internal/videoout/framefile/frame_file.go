// Package framefile composites UI over decoder frame files for terminal output.
package framefile

import (
	"io"
	"os"
	"time"

	"misterfin-crt/internal/platform"
	"misterfin-crt/internal/ui"
	"misterfin-crt/internal/videoout"
)

// Backend implements videoout.Output.
type Backend struct {
	d      platform.Presenter
	source string
	watch  *frameWatch
	frame  []byte // Refilled from the clean decoder file before every composition.
}

// New composites UI over clean decoder frames published at source.
func New(d platform.Presenter, source string) *Backend {
	return &Backend{d: d, source: source}
}

// Acquire is a no-op because the decoder writes a private frame file.
func (o *Backend) Acquire() {}

// Release is a no-op because the decoder never owns the display.
func (o *Backend) Release() {}

// Clear removes stale decoder output before loading or returning to browsing.
func (o *Backend) Clear() { _ = os.Remove(o.source) }

// Close removes the source and stops frame notifications without closing the display.
func (o *Backend) Close() error {
	o.Clear()
	if o.watch != nil {
		return o.watch.close()
	}
	return nil
}

// FrameUpdates watches complete decoder publications, allowing presentation to
// follow video arrival rather than waiting for the next animation tick.
func (o *Backend) FrameUpdates() (<-chan struct{}, error) {
	if o.watch == nil {
		watch, err := watchFrames(o.source)
		if err != nil {
			return nil, err
		}
		o.watch = watch
	}
	return o.watch.updates, nil
}

// Geometry reports the underlying display dimensions.
func (o *Backend) Geometry() platform.Geometry { return o.d.Geometry() }

// FrameInterval leaves headroom to sample every frame of a 24–30 fps stream.
// Sampling at the stream's own rate can skip frames when the two clocks drift.
func (o *Backend) FrameInterval(bool) time.Duration { return time.Second / 60 }

// Present reads clean video into reusable storage before drawing the overlay.
// Missing or malformed frames use black pixels. Decoder files remain unchanged.
func (o *Backend) Present(f videoout.Frame) error {
	if !f.Video {
		return o.d.Present(f.UI)
	}
	g := o.d.Geometry()
	size := g.Width * g.Height * 4
	if len(o.frame) != size+1 {
		o.frame = make([]byte, size+1)
	}
	frame := o.frame[:size]
	file, err := os.Open(o.source)
	if err != nil {
		clear(frame)
	} else {
		// The extra byte detects oversized publications without buffering them.
		n, readErr := io.ReadFull(file, o.frame)
		_ = file.Close()
		if n != size || readErr != io.ErrUnexpectedEOF {
			clear(frame)
		}
	}
	ui.Composite(frame, f.Overlay)
	return o.d.Present(frame)
}

var _ videoout.Output = (*Backend)(nil)
