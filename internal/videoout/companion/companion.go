// Package companion presents UI beside a separate player window.
package companion

import (
	"time"

	"mistervision/internal/platform"
	"mistervision/internal/ui"
	"mistervision/internal/videoout"
)

// Backend implements videoout.Output.
type Backend struct {
	d platform.Presenter
}

// New presents playback UI beside a player that owns another window.
func New(d platform.Presenter) *Backend { return &Backend{d: d} }

// Acquire is a no-op because the decoder owns a separate window.
func (o *Backend) Acquire() {}

// Release is a no-op because the UI presenter is never transferred to the decoder.
func (o *Backend) Release() {}

// Clear is a no-op. The next scene replaces this backend's presentation.
func (o *Backend) Clear() {}

// Close releases no resources. The caller owns the borrowed presenter.
func (o *Backend) Close() error {
	return nil
}

// Geometry reports the borrowed presenter's logical and physical dimensions.
func (o *Backend) Geometry() platform.Geometry { return o.d.Geometry() }

// FrameInterval follows browser motion at 60 Hz and playback controls at 30 Hz.
// The separate player window owns video presentation.
func (o *Backend) FrameInterval(video bool) time.Duration {
	if video {
		return time.Second / 30
	}
	return time.Second / 60
}

// Present draws UI and, during video, a composited controls frame beside the
// decoder window. Input pixels are borrowed only until Present returns.
func (o *Backend) Present(f videoout.Frame) error {
	if !f.Video {
		return o.d.Present(f.UI)
	}
	frame := append([]byte(nil), f.UI...)
	ui.Composite(frame, f.Overlay)
	return o.d.Present(frame)
}

var _ videoout.Output = (*Backend)(nil)
