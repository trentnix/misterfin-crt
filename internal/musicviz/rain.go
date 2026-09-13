package musicviz

import (
	"math"

	"misterfin-go/internal/ui"
)

type rain struct {
	preset Preset
	phase  float64
}

func (r *rain) Draw(c *ui.Canvas, f Frame, dt float64) {
	r.phase += dt
	for i := 0; i < r.preset.Density; i++ {
		speed := .3 + float64(i%5)*.08
		y := int(math.Mod(float64(i)*.618034+r.phase*speed, 1)*float64(c.Height+30)) - 15
		x := (i*137 + int(r.phase*9)) % c.Width
		line(c, x, y, x-4, y+8+i%8, tint(r.preset.color, *r.preset.Intensity*(.35+float64(i%5)*.13)))
	}
}
