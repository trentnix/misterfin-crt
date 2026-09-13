// Package framefile composites UI over decoder frame files for terminal output.
package framefile

import (
	"os"
	"time"

	"misterfin-go/internal/platform"
	"misterfin-go/internal/ui"
	"misterfin-go/internal/videoout"
)

// Backend implements videoout.Output.
type Backend struct {
	d      platform.Presenter
	source string
	watch  *frameWatch
}

// New composites UI over clean decoder frames published at source.
func New(d platform.Presenter, source string) *Backend {
	return &Backend{d: d, source: source}
}

func (o *Backend) Acquire() {}
func (o *Backend) Release() {}
func (o *Backend) Clear()   { _ = os.Remove(o.source) }
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
func (o *Backend) Geometry() platform.Geometry { return o.d.Geometry() }

// FrameInterval leaves headroom to sample every frame of a 24–30 fps stream.
// Sampling at the stream's own rate can skip frames when the two clocks drift.
func (o *Backend) FrameInterval(bool) time.Duration { return time.Second / 60 }

func (o *Backend) Present(f videoout.Frame) error {
	if !f.Video {
		return o.d.Present(f.UI)
	}
	g := o.d.Geometry()
	frame, err := os.ReadFile(o.source)
	if err != nil || len(frame) != g.Width*g.Height*4 {
		frame = make([]byte, g.Width*g.Height*4)
	}
	ui.Composite(frame, f.Overlay)
	return o.d.Present(frame)
}

var _ videoout.Output = (*Backend)(nil)
