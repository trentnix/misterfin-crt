// Package framefile composites UI over decoder frame files for terminal output.
package framefile

import (
	"os"

	"misterfin-go/internal/platform"
	"misterfin-go/internal/ui"
	"misterfin-go/internal/videoout"
)

// Backend implements videoout.Output.
type Backend struct {
	d      platform.Presenter
	source string
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
	return nil
}
func (o *Backend) Geometry() platform.Geometry { return o.d.Geometry() }
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
