package ui

import "image"

// Overlay composites an unscaled straight-alpha image onto either kind of
// canvas. It clips to the canvas and preserves antialiasing and transparency.
func (c *Canvas) Overlay(source *image.NRGBA, x, y int) {
	if source == nil {
		return
	}
	bounds := source.Bounds()
	for yy := max(0, y); yy < min(c.Height, y+bounds.Dy()); yy++ {
		for xx := max(0, x); xx < min(c.Width, x+bounds.Dx()); xx++ {
			s := (yy-y)*source.Stride + (xx-x)*4
			a := int(source.Pix[s+3])
			if a == 0 {
				continue
			}
			d := (yy*c.Width + xx) * 4
			old := 255
			if c.transparent {
				old = int(c.Pixels[d+3])
			}
			out := a + (old*(255-a)+127)/255
			for k := 0; k < 3; k++ {
				value := int(source.Pix[s+2-k])*a + (int(c.Pixels[d+k])*old*(255-a)+127)/255
				c.Pixels[d+k] = byte(min(255, (value+out/2)/out))
			}
			if c.transparent {
				c.Pixels[d+3] = byte(out)
			}
		}
	}
}
