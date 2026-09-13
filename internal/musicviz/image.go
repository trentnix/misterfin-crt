package musicviz

import (
	"image"
	"time"

	"misterfin-go/internal/ui"
)

type imageEffect struct {
	preset  Preset
	elapsed time.Duration
}

func (e *imageEffect) Draw(c *ui.Canvas, f Frame, dt float64) {
	e.elapsed += time.Duration(dt * float64(time.Second))
	if frame := assetFrame(e.preset, e.elapsed); frame != nil {
		c.Image(frame, 0, 0, c.Width, c.Height)
		c.Shade(0, 0, c.Width, c.Height, int((1-*e.preset.Intensity)*255))
	}
}
func assetFrame(p Preset, elapsed time.Duration) image.Image {
	if len(p.frames) == 0 {
		return nil
	}
	total := time.Duration(0)
	for _, d := range p.delays {
		total += d
	}
	elapsed %= total
	for i, d := range p.delays {
		if elapsed < d {
			return p.frames[i]
		}
		elapsed -= d
	}
	return p.frames[0]
}
