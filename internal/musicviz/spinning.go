package musicviz

import (
	"image"
	"math"

	"misterfin-crt/internal/ui"
)

// spinning caches a small square cover and rotates samples inside a disc mask.
// Its bars represent stereo energy, not a frequency spectrum.
type spinning struct {
	preset Preset
	phase  float64
	source image.Image
	cover  *image.RGBA
	disc   *image.RGBA
}

func (s *spinning) Draw(c *ui.Canvas, f Frame, dt float64) {
	if !f.Paused {
		s.phase += dt * .5
	}
	if f.Artwork != s.source {
		s.source = f.Artwork
		s.cover = nil
		if f.Artwork != nil {
			s.cover = image.NewRGBA(image.Rect(0, 0, 128, 128))
			b := f.Artwork.Bounds()
			for y := 0; y < 128; y++ {
				for x := 0; x < 128; x++ {
					s.cover.Set(x, y, f.Artwork.At(b.Min.X+x*b.Dx()/128, b.Min.Y+y*b.Dy()/128))
				}
			}
		}
	}
	for i := 0; i < 32; i++ {
		v := f.Levels[i%2]
		height := int((.15 + v*.85) * float64(c.Height/3))
		c.Rect(i*c.Width/32, c.Height*2/3-height, c.Width/32-3, height, tint(s.preset.color, *s.preset.Intensity*.25))
	}
	if s.cover == nil {
		return
	}
	if s.disc == nil {
		s.disc = image.NewRGBA(image.Rect(0, 0, 96, 96))
	}
	co, si := int(math.Cos(s.phase)*65536), int(math.Sin(s.phase)*65536)
	for y := 0; y < 96; y++ {
		fy := y - 48
		for x := 0; x < 96; x++ {
			fx := x - 48
			d := fx*fx + fy*fy
			if d > 47*47 || d < 42 {
				continue
			}
			sx := min(127, max(0, 64+((fx*co-fy*si)*4/3>>16)))
			sy := min(127, max(0, 64+((fx*si+fy*co)*4/3>>16)))
			src := sy*s.cover.Stride + sx*4
			dst := y*s.disc.Stride + x*4
			copy(s.disc.Pix[dst:dst+4], s.cover.Pix[src:src+4])
		}
	}

	box := f.ArtworkBounds
	if box.Empty() {
		box = image.Rect(c.Width/4, c.Height/6, c.Width*3/4, c.Height*3/5)
	}
	c.Image(s.disc, box.Min.X, box.Min.Y, box.Dx(), box.Dy())
}
