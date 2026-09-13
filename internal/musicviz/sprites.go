package musicviz

import (
	"math"
	"time"

	"misterfin-crt/internal/ui"
)

// sprites reuses decoded PNG/GIF frames across a small layered formation.
type sprites struct {
	preset Preset
	phase  float64
	canvas *ui.Canvas
}

func (s *sprites) Draw(c *ui.Canvas, f Frame, dt float64) {
	// Sprite motion uses a smaller background canvas. Text and controls still
	// render at full UI resolution after this pass.
	w, h := max(1, c.Width/2), max(1, c.Height/2)
	if s.canvas == nil || s.canvas.Width != w || s.canvas.Height != h {
		s.canvas = ui.New(w, h)
	} else {
		clear(s.canvas.Pixels)
	}
	s.drawFormation(s.canvas, f, dt)
	if c.Width == 2*w && c.Height == 2*h {
		for y := 0; y < h; y++ {
			src := s.canvas.Pixels[y*w*4 : (y+1)*w*4]
			row := c.Pixels[y*2*c.Width*4 : (y*2+1)*c.Width*4]
			for x := 0; x < w; x++ {
				pixel := src[x*4 : x*4+4]
				copy(row[x*8:x*8+4], pixel)
				copy(row[x*8+4:x*8+8], pixel)
			}
			copy(c.Pixels[(y*2+1)*c.Width*4:(y*2+2)*c.Width*4], row)
		}
		return
	}
	for y := 0; y < c.Height; y++ {
		for x := 0; x < c.Width; x++ {
			src := (y*h/c.Height*w + x*w/c.Width) * 4
			dst := (y*c.Width + x) * 4
			copy(c.Pixels[dst:dst+4], s.canvas.Pixels[src:src+4])
		}
	}

}

func (s *sprites) drawFormation(c *ui.Canvas, f Frame, dt float64) {
	s.phase += dt
	for i := 0; i < min(15, s.preset.Density); i++ {
		scale := .5 + float64(i%3)*.35
		w := int(90 * scale * float64(c.Width) / 640)
		h := max(1, w*c.Height*4/(c.Width*3))
		x := int(math.Mod(float64(i)*.618034+s.phase*(.025+scale*.02), 1)*float64(c.Width+w)) - w
		y := (i*47)%(max(1, c.Height-h)) + int(math.Sin(s.phase*2+float64(i))*4)
		preset := s.preset
		if len(preset.groups) > 0 {
			preset = preset.groups[i%len(preset.groups)]
		}
		frame := assetFrame(preset, time.Duration((s.phase+float64(i)*.2)*float64(time.Second)))
		c.Image(frame, x, y, w, h)
	}
	c.Shade(0, 0, c.Width, c.Height, int((1-*s.preset.Intensity)*255))
}
