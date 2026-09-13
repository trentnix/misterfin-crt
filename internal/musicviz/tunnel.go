package musicviz

import (
	"math"

	"misterfin-go/internal/ui"
)

type tunnel struct {
	preset Preset
	phase  float64
}

func (t *tunnel) Draw(c *ui.Canvas, f Frame, dt float64) {
	t.phase += dt * (.22 + energy(f))
	for ring := 0; ring < 12; ring++ {
		z := math.Mod(float64(ring)/12+t.phase, 1)
		radius := float64(c.Width) * .018 / max(.025, 1-z)
		col := tint(t.preset.color, *t.preset.Intensity*z)
		for side := 0; side < 8; side++ {
			a := float64(side)*math.Pi/4 + t.phase*.25
			b := a + math.Pi/4
			x := c.Width/2 + int(math.Cos(a)*radius)
			y := c.Height/2 + int(math.Sin(a)*radius*float64(c.Height)/float64(c.Width))
			xx := c.Width/2 + int(math.Cos(b)*radius)
			yy := c.Height/2 + int(math.Sin(b)*radius*float64(c.Height)/float64(c.Width))
			line(c, x, y, xx, yy, col)
		}
	}
}
