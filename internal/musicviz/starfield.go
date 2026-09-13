package musicviz

import (
	"math"

	"misterfin-crt/internal/ui"
)

// starfield projects deterministic stars at shrinking depths. Motion uses time,
// so different display refresh rates do not change its travel speed.
type starfield struct {
	preset Preset
	phase  float64
}

func (s *starfield) Draw(c *ui.Canvas, f Frame, dt float64) {
	s.phase += dt * .14
	for i := 0; i < s.preset.Density; i++ {
		angle := float64(i) * 2.39996
		z := 1 - math.Mod(float64(i)*.618034+s.phase, 1)
		z = max(.04, z)
		radius := float64((i*17)%40 + 12)
		x := c.Width/2 + int(math.Cos(angle)*radius/z)
		y := c.Height/2 + int(math.Sin(angle)*radius/z*float64(c.Height)/float64(c.Width))
		if x < 0 || y < 0 || x >= c.Width || y >= c.Height {
			continue
		}
		col := tint(s.preset.color, *s.preset.Intensity*(1-z*.6))
		line(c, x, y, c.Width/2+int(float64(x-c.Width/2)*.96), c.Height/2+int(float64(y-c.Height/2)*.96), col)
	}
}
