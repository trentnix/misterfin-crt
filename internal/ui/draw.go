// Package ui draws browser text and artwork in Go.
package ui

import (
	"image"
	"strings"
)

// Canvas owns a tightly packed pixel buffer. Ordinary canvases store BGRX,
// while overlays store straight-alpha BGRA. Drawing is clipped to the canvas.
// Callers must serialize drawing and must not resize Pixels or change dimensions.
type Canvas struct {
	Width, Height int
	Pixels        []byte
	transparent   bool
}

// New allocates a black BGRX canvas. Dimensions must be positive and their
// four-byte pixel storage must fit in memory. The caller owns the returned buffer.
func New(w, h int) *Canvas {
	return &Canvas{Width: w, Height: h, Pixels: make([]byte, w*h*4)}
}

// NewOverlay allocates a transparent BGRA canvas with the same size requirements
// as New. Rect and Text make covered pixels opaque. Shade adds translucent black.
func NewOverlay(w, h int) *Canvas {
	return &Canvas{Width: w, Height: h, Pixels: make([]byte, w*h*4), transparent: true}
}

// Rect fills the clipped rectangle with color in 0xRRGGBB format.
func (c *Canvas) Rect(x, y, w, h int, color uint32) {
	for yy := max(0, y); yy < min(c.Height, y+h); yy++ {
		for xx := max(0, x); xx < min(c.Width, x+w); xx++ {
			i := (yy*c.Width + xx) * 4
			c.Pixels[i] = byte(color)
			c.Pixels[i+1] = byte(color >> 8)
			c.Pixels[i+2] = byte(color >> 16)
			if c.transparent {
				c.Pixels[i+3] = 255
			}
		}
	}
}

// Text draws one line with 8x8 glyphs and an embedded Unicode fallback. maxWidth is the absolute right edge,
// not a character count or width relative to x. Unsupported runes become question marks.
func (c *Canvas) Text(x, y int, s string, color uint32, maxWidth int) {
	c.TextScaled(x, y, s, color, maxWidth, 1)
}

// TextScaled draws Text at a positive integer scale. It stops before the first
// glyph that exceeds maxWidth or the canvas right edge, and ignores later lines.
func (c *Canvas) TextScaled(x, y int, s string, color uint32, maxWidth, scale int) {
	for _, r := range s {
		if x+8*scale > min(c.Width, maxWidth) {
			break
		}
		if r == '\n' {
			break
		}
		glyph := glyphForRune(r)
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

// Wrap word-wraps text within a pixel width using at most lines rows, spaced
// ten pixels apart. Whitespace is collapsed and overlong words are clipped.
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

// Image centers artwork in the supplied box, preserving its aspect ratio on a
// physical 4:3 screen. Nil is a no-op. Use this on opaque canvases.
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

// Blit scales the complete image into a box and blends its source alpha into
// the destination colors. It leaves destination alpha unchanged and is intended
// for opaque canvases. Nil images and nonpositive boxes are no-ops.
func (c *Canvas) Blit(im image.Image, x, y, w, h int) {
	if im == nil || w <= 0 || h <= 0 {
		return
	}
	if rgba, ok := im.(*image.RGBA); ok {
		c.blitRGBA(rgba, x, y, w, h)
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

// Shade adds black over the clipped rectangle. alpha must be between 0
// (unchanged) and 255 (black). Overlay canvases also accumulate coverage.
func (c *Canvas) Shade(x, y, w, h, alpha int) {
	if !c.transparent {
		// Reuse the same channel transform instead of dividing every pixel on ARM.
		var shaded [256]byte
		for v := range shaded {
			shaded[v] = byte(v * (255 - alpha) / 255)
		}
		for yy := max(0, y); yy < min(c.Height, y+h); yy++ {
			for xx := max(0, x); xx < min(c.Width, x+w); xx++ {
				i := (yy*c.Width + xx) * 4
				c.Pixels[i] = shaded[c.Pixels[i]]
				c.Pixels[i+1] = shaded[c.Pixels[i+1]]
				c.Pixels[i+2] = shaded[c.Pixels[i+2]]
			}
		}
		return
	}
	for yy := max(0, y); yy < min(c.Height, y+h); yy++ {
		for xx := max(0, x); xx < min(c.Width, x+w); xx++ {
			i := (yy*c.Width + xx) * 4
			oldAlpha := int(c.Pixels[i+3])
			outAlpha := alpha + oldAlpha*(255-alpha)/255
			if outAlpha > 0 {
				for k := 0; k < 3; k++ {
					c.Pixels[i+k] = byte(int(c.Pixels[i+k]) * oldAlpha * (255 - alpha) / (255 * outAlpha))
				}
			}
			c.Pixels[i+3] = byte(outAlpha)
		}
	}
}

// Composite draws a straight-alpha BGRA overlay over a BGRX frame.
func Composite(frame, overlay []byte) {
	if len(frame) != len(overlay) || len(frame)%4 != 0 {
		return
	}
	for i := 0; i < len(frame); i += 4 {
		a := int(overlay[i+3])
		if a == 0 {
			continue
		}
		for k := 0; k < 3; k++ {
			frame[i+k] = byte((int(overlay[i+k])*a + int(frame[i+k])*(255-a) + 127) / 255)
		}
	}
}

// blitRGBA avoids interface calls and boxed colors for cached browser artwork.
// Source pixels are premultiplied RGBA. The destination is BGRX, as in Blit.
func (c *Canvas) blitRGBA(im *image.RGBA, x, y, w, h int) {
	left, right := max(0, x), min(c.Width, x+w)
	top, bottom := max(0, y), min(c.Height, y+h)
	if left >= right || top >= bottom {
		return
	}
	offsets := make([]int, right-left)
	for xx := range offsets {
		offsets[xx] = (left + xx - x) * im.Rect.Dx() / w * 4
	}
	for yy := top; yy < bottom; yy++ {
		row := (yy - y) * im.Rect.Dy() / h * im.Stride
		dst := (yy*c.Width + left) * 4
		for _, offset := range offsets {
			src := row + offset
			a := uint32(im.Pix[src+3])
			if a == 255 {
				c.Pixels[dst], c.Pixels[dst+1], c.Pixels[dst+2] = im.Pix[src+2], im.Pix[src+1], im.Pix[src]
			} else {
				for k := 0; k < 3; k++ {
					c.Pixels[dst+k] = byte(min(255, uint32(im.Pix[src+2-k])+uint32(c.Pixels[dst+k])*(255-a)/255))
				}
			}
			dst += 4
		}
	}
}
