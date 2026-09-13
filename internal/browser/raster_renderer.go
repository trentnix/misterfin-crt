package browser

import (
	"misterfin-go/internal/ui"
	"misterfin-go/internal/videoout"
)

// RasterRenderer belongs to the browser event loop. Its returned pixels are
// borrowed until the next draw, matching Output.Present's synchronous contract.
// Artwork from artworkLoader is immutable after publication.
type RasterRenderer struct {
	canvas    *ui.Canvas
	cache     sceneCache
	overlay   *ui.Canvas
	animation animationState
}

// NewRenderer constructs the shared software renderer used by all destinations.
func NewRenderer() *RasterRenderer { return &RasterRenderer{} }

func (r *RasterRenderer) Render(w, h int, s Scene) videoout.Frame {
	r.prepare(w, h)
	anim := r.animation.advance(s, visibleRows(w, h))
	f := videoout.Frame{UI: renderScene(r.canvas, &r.cache, s, anim), Video: s.Video}
	if s.Video {
		clear(r.overlay.Pixels)
		f.Overlay = renderVideoOverlayOn(r.overlay, s.Playback, s.Now, s.Controls)
	}
	return f
}

var _ Renderer = (*RasterRenderer)(nil)

// prepare owns geometry invalidation for both frame buffers and artwork caches.
func (r *RasterRenderer) prepare(w, h int) {
	if r.canvas == nil || r.canvas.Width != w || r.canvas.Height != h {
		r.canvas = ui.New(w, h)
		r.overlay = ui.NewOverlay(w, h)
		r.cache = sceneCache{}
	} else {
		clear(r.canvas.Pixels)
	}
}
