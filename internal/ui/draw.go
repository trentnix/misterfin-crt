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
	c.TextScaled(x, y, s, color, maxWidth, 1)
}
func (c *Canvas) TextScaled(x, y int, s string, color uint32, maxWidth, scale int) {
	for _, r := range s {
		if x+8*scale > min(c.Width, maxWidth) {
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
					c.Rect(x+xx*scale, y+yy*scale, scale, scale, color)
				}
			}
		}
		x += 8 * scale
	}
}
func (c *Canvas) Wrap(x, y, width, lines int, s string, color uint32) {
	var line string
	for _, word := range strings.Fields(s) {
		if len([]rune(line+word))*8 > width && line != "" {
			c.Text(x, y, line, color, x+width)
			y += 10
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
	c.Blit(im, x, y, dw, dh)
}

// Blit scales the complete image into a box and preserves transparency.
func (c *Canvas) Blit(im image.Image, x, y, w, h int) {
	if im == nil || w <= 0 || h <= 0 {
		return
	}
	b := im.Bounds()
	for yy := max(0, y); yy < min(c.Height, y+h); yy++ {
		for xx := max(0, x); xx < min(c.Width, x+w); xx++ {
			r, g, blue, a := im.At(b.Min.X+(xx-x)*b.Dx()/w, b.Min.Y+(yy-y)*b.Dy()/h).RGBA()
			i := (yy*c.Width + xx) * 4
			for k, v := range []uint32{blue, g, r} {
				c.Pixels[i+k] = byte(min(255, (v+uint32(c.Pixels[i+k])*(65535-a)/255)/257))
			}
		}
	}
}
func (c *Canvas) Shade(x, y, w, h, alpha int) {
	for yy := max(0, y); yy < min(c.Height, y+h); yy++ {
		for xx := max(0, x); xx < min(c.Width, x+w); xx++ {
			i := (yy*c.Width + xx) * 4
			for k := 0; k < 3; k++ {
				c.Pixels[i+k] = byte(int(c.Pixels[i+k]) * (255 - alpha) / 255)
			}
		}
	}
}
