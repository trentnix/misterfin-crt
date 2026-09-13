// Package companion presents UI beside a separate player window.
package companion

import (
	"time"

	"misterfin-go/internal/platform"
	"misterfin-go/internal/ui"
	"misterfin-go/internal/videoout"
)

// Backend implements videoout.Output.
type Backend struct {
	d platform.Presenter
}

// New presents playback UI beside a player that owns another window.
func New(d platform.Presenter) *Backend { return &Backend{d: d} }

func (o *Backend) Acquire() {}
func (o *Backend) Release() {}
func (o *Backend) Clear()   {}
func (o *Backend) Close() error {
	return nil
}
func (o *Backend) Geometry() platform.Geometry { return o.d.Geometry() }

// FrameInterval follows browser motion at 60 Hz and playback controls at 30 Hz.
// The separate player window owns video presentation.
func (o *Backend) FrameInterval(video bool) time.Duration {
	if video {
		return time.Second / 30
	}
	return time.Second / 60
}

func (o *Backend) Present(f videoout.Frame) error {
	if !f.Video {
		return o.d.Present(f.UI)
	}
	frame := append([]byte(nil), f.UI...)
	ui.Composite(frame, f.Overlay)
	return o.d.Present(frame)
}

var _ videoout.Output = (*Backend)(nil)
