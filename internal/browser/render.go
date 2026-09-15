package browser

import (
	"image"
	"time"

	"misterfin-crt/internal/musicviz"
	"misterfin-crt/internal/ui"
)

// Render draws one uncached navigation frame and returns an owned BGRX buffer.
// For playback, supply a Scene with a PlaybackPresentation to Renderer.Render.
// The application uses [RasterRenderer] to reuse canvases, animation, and artwork.
func Render(w, h int, m *Model, setup SetupPresentation, art image.Image, artError string) []byte {
	return render(w, h, m, setup, Artwork{Primary: art}, artError, Animation{Selection: float64(m.Current().Selected), Row: float64(m.Current().Selected - m.Current().Scroll)}, time.Now())
}

func render(w, h int, m *Model, setup SetupPresentation, art Artwork, artError string, anim Animation, now time.Time) []byte {
	return renderScene(ui.New(w, h), nil, sceneFromModel(m, PlaybackPresentation{}, setup, selectionData{artwork: art}, artError, now), anim)
}

// renderScene selects exactly one screen. Browsing screens share footer and
// notice drawing. Media and connection screens supply their own chrome.
func renderScene(c *ui.Canvas, cache *sceneCache, s Scene, anim Animation) []byte {
	return renderSceneWithMusic(c, cache, s, anim, nil)
}

func renderSceneWithMusic(c *ui.Canvas, cache *sceneCache, s Scene, anim Animation, music *musicviz.Renderer) []byte {
	sy := safeY(c.Width, c.Height)
	p := screenPainter{
		canvas: c, cache: cache, scene: s, animation: anim, visualizer: music,
		width: c.Width, height: c.Height, safeY: sy, bottom: c.Height - 8 - sy,
	}
	switch {
	case s.About.Visible:
		p.about()
	case s.Setup.Kind != SetupHidden:
		p.setup()
	case s.View.Detail != nil && s.View.Detail.Type == "Photo":
		p.photo()
	case s.Video && s.View.Detail != nil:
		p.videoBackdrop()
	case s.Audio && s.View.Detail != nil:
		p.music()
	default:
		var controls [][]controlHint
		switch {
		case s.View.Detail != nil:
			controls = p.details()
		case s.Root && !s.ListMode:
			controls = p.carousel()
		default:
			controls = p.list()
		}
		p.footer(controls)
	}
	return c.Pixels
}
