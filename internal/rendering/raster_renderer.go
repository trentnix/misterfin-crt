package rendering

import (
	"misterfin-crt/internal/musicviz"
	"misterfin-crt/internal/ui"
	"misterfin-crt/internal/videoout"
)

// RasterRenderer draws shared UI and video overlays in memory. The zero value is
// ready to use. Calls must be serial, and source artwork must remain immutable.
// Returned pixels are borrowed until the next Render call.
type RasterRenderer struct {
	music     musicviz.Renderer
	canvas    *ui.Canvas
	cache     sceneCache
	overlay   *ui.Canvas
	animation animationState
}

// NewRenderer constructs the shared software renderer used by all destinations.
func NewRenderer() *RasterRenderer { return &RasterRenderer{} }

// Render draws one scene at positive logical dimensions. It reuses frame storage,
// advances animation from Scene.Now, and invalidates visual caches on size changes.
// The caller must present or copy both pixel slices before calling Render again.
func (r *RasterRenderer) Render(w, h int, s Scene) videoout.Frame {
	r.prepare(w, h)
	anim := r.animation.advance(s, VisibleRows(w, h))
	f := videoout.Frame{UI: renderSceneWithMusic(r.canvas, &r.cache, s, anim, &r.music), Video: s.Video}
	if s.Video {
		clear(r.overlay.Pixels)
		f.Overlay = renderVideoOverlayOn(r.overlay, s.Playback, s.Now, s.Controls)
		drawMessage(r.overlay, s.Message, s.Now)
	}
	drawMessage(r.canvas, s.Message, s.Now)
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
