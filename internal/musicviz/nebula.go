package musicviz

import (
	"math"

	"misterfin-crt/internal/ui"
)

// nebula evaluates a small plasma field. Shared sine terms and a color table
// keep trigonometry and color conversion out of the pixel loop on ARM.
type nebula struct {
	preset  Preset
	phase   float64
	palette [256]uint32
	ready   bool
}

func (n *nebula) Draw(c *ui.Canvas, f Frame, dt float64) {
	if !n.ready {
		for i := range n.palette {
			v := float64(i) / 255
			col := uint32(40+100*v)<<16 | uint32(20+70*(1-v))<<8 | uint32(80+150*v)
			if n.preset.Color != "" {
				col = n.preset.color
			}
			n.palette[i] = tint(col, *n.preset.Intensity*(.2+v*.8))
		}
		n.ready = true
	}
	n.phase += dt * (.6 + energy(f)*3)
	var xs [80]float64
	var ys [45]float64
	var diagonals [124]float64
	for x := range xs {
		xs[x] = math.Sin(float64(x)*.12 + n.phase)
	}
	for y := range ys {
		ys[y] = math.Sin(float64(y)*.16 - n.phase*.7)
	}
	for i := range diagonals {
		diagonals[i] = math.Sin(float64(i)*.08 + n.phase*.4)
	}
	for y := 0; y < 45; y++ {
		top, bottom := y*c.Height/45, (y+1)*c.Height/45
		if top == bottom {
			continue
		}
		row := c.Pixels[top*c.Width*4 : (top+1)*c.Width*4]
		for x := 0; x < 80; x++ {
			col := n.palette[min(255, max(0, int((xs[x]+ys[y]+diagonals[x+y]+3)*42.5)))]
			for xx := x * c.Width / 80; xx < (x+1)*c.Width/80; xx++ {
				i := xx * 4
				row[i], row[i+1], row[i+2] = byte(col), byte(col>>8), byte(col>>16)
			}
		}
		for yy := top + 1; yy < bottom; yy++ {
			copy(c.Pixels[yy*c.Width*4:(yy+1)*c.Width*4], row)
		}
	}
}
