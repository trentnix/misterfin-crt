package rendering

import (
	"mistervision/internal/musicviz"
	"mistervision/internal/ui"
)

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
	case s.Content.Detail != nil && s.Content.Detail.Type == "Photo":
		p.photo()
	case s.Video && s.Content.Detail != nil:
		p.videoBackdrop()
	case s.Audio && s.Content.Detail != nil:
		p.music()
	default:
		var controls [][]controlHint
		switch {
		case s.Content.Detail != nil:
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
