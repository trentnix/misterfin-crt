package browser

import (
	"image"
	"time"

	"misterfin-go/internal/ui"
)

// Render draws one uncached browser frame and returns an owned BGRX buffer.
// The application uses [RasterRenderer] to reuse canvases, animation, and artwork.
func Render(w, h int, m *Model, status string, art image.Image, artError string) []byte {
	return render(w, h, m, status, Artwork{Primary: art}, artError, Animation{Selection: float64(m.Current().Selected), Row: float64(m.Current().Selected - m.Current().Scroll)}, time.Now())
}

func render(w, h int, m *Model, status string, art Artwork, artError string, anim Animation, now time.Time) []byte {
	return renderScene(ui.New(w, h), nil, sceneFromModel(m, status, art, artError, now), anim)
}

// renderScene selects exactly one screen. Browsing screens share footer and
// notice drawing. Media and connection screens supply their own chrome.
func renderScene(c *ui.Canvas, cache *sceneCache, s Scene, anim Animation) []byte {
	sy := safeY(c.Width, c.Height)
	p := screenPainter{
		canvas: c, cache: cache, scene: s, animation: anim,
		width: c.Width, height: c.Height, safeY: sy, bottom: c.Height - 8 - sy,
	}
	switch {
	case s.Status != "":
		p.status()
	case s.View.Detail != nil && s.View.Detail.Type == "Photo":
		p.photo()
	case s.Video && s.View.Detail != nil:
		p.videoBackdrop()
	case s.Audio && s.View.Detail != nil:
		p.music()
	default:
		var hint string
		switch {
		case s.View.Detail != nil:
			hint = p.details()
		case s.Root && !s.ListMode:
			hint = p.carousel()
		default:
			hint = p.list()
		}
		p.footer(hint)
	}
	return c.Pixels
}
