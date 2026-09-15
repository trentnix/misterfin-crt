package rendering

import (
	"image"
	"math"

	"misterfin-crt/internal/ui"
)

// customBackground caches a dimmed, centered crop in physical 4:3 screen space.
// Carousel and list frames share the prepared pixels. Source images are immutable.
func (s *sceneCache) customBackground(c *ui.Canvas, source image.Image) {
	if s == nil {
		drawCustomBackground(c, source)
		return
	}
	if s.customBase == nil || s.customBase.Width != c.Width || s.customBase.Height != c.Height || !sameArtwork(s.customSource, source) {
		s.customBase = ui.New(c.Width, c.Height)
		drawCustomBackground(s.customBase, source)
		s.customSource = source
	}
	copy(c.Pixels, s.customBase.Pixels)
}

func drawCustomBackground(c *ui.Canvas, source image.Image) {
	b := source.Bounds()
	if b.Empty() {
		return
	}
	par := float64(c.Width) * 3 / float64(c.Height*4)
	scale := max(float64(c.Width)/float64(b.Dx()), float64(c.Height)*par/float64(b.Dy()))
	w := int(math.Ceil(float64(b.Dx()) * scale))
	h := int(math.Ceil(float64(b.Dy()) * scale / par))
	c.Blit(source, (c.Width-w)/2, (c.Height-h)/2, w, h)
	c.Shade(0, 0, c.Width, c.Height, 145)
}
