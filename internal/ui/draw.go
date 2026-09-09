// Package ui draws browser text and artwork in Go.
package ui

import (
	"image"
	"strings"
)

type Canvas struct {
	Width, Height int
	Pixels        []byte
}

func New(w, h int) *Canvas { return &Canvas{w, h, make([]byte, w*h*4)} }
func (c *Canvas) Rect(x, y, w, h int, color uint32) {
	for yy := max(0, y); yy < min(c.Height, y+h); yy++ {
		for xx := max(0, x); xx < min(c.Width, x+w); xx++ {
			i := (yy*c.Width + xx) * 4
			c.Pixels[i] = byte(color)
			c.Pixels[i+1] = byte(color >> 8)
			c.Pixels[i+2] = byte(color >> 16)
		}
	}
}
func (c *Canvas) Text(x, y int, s string, color uint32, maxWidth int) {
	for _, r := range s {
		if x+8 > min(c.Width, maxWidth) {
			break
		}
		if r == '\n' {
			break
		}
		if r < 32 {
			r = ' '
		}
		if r > 255 {
			r = '?'
		}
		glyph := font[r]
		for yy, bits := range glyph {
			for xx := 0; xx < 8; xx++ {
				if bits&(1<<xx) != 0 {
					c.Rect(x+xx, y+yy, 1, 1, color)
				}
			}
		}
		x += 9
	}
}
func (c *Canvas) Wrap(x, y, width, lines int, s string, color uint32) {
	var line string
	for _, word := range strings.Fields(s) {
		if len([]rune(line+word))*9 > width && line != "" {
			c.Text(x, y, line, color, x+width)
			y += 12
			lines--
			line = ""
			if lines == 0 {
				return
			}
		}
		line += word + " "
	}
	if lines > 0 {
		c.Text(x, y, line, color, x+width)
	}
}
func (c *Canvas) Image(im image.Image, x, y, w, h int) {
	if im == nil {
		return
	}
	b := im.Bounds()
	// Logical CRT pixels are tall. Fit artwork in physical 4:3 display space.
	par := float64(c.Width) * 3 / float64(c.Height*4)
	scale := min(float64(w)/float64(b.Dx()), float64(h)*par/float64(b.Dy()))
	dw := max(1, int(float64(b.Dx())*scale))
	dh := max(1, int(float64(b.Dy())*scale/par))
	x += (w - dw) / 2
	y += (h - dh) / 2
	for yy := 0; yy < dh; yy++ {
		for xx := 0; xx < dw; xx++ {
			r, g, bb, _ := im.At(b.Min.X+xx*b.Dx()/dw, b.Min.Y+yy*b.Dy()/dh).RGBA()
			c.Rect(x+xx, y+yy, 1, 1, uint32(r>>8)<<16|uint32(g>>8)<<8|uint32(bb>>8))
		}
	}
}
