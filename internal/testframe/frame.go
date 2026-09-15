// Package testframe draws a deterministic frame in Go through the display interface.
package testframe

import "misterfin-crt/internal/platform"

// Present draws color bars, a grayscale ramp, and a border in the display's
// logical geometry. It borrows d and returns its presentation error.
func Present(d platform.Display) error {
	g := d.Geometry()
	pixels := make([]byte, g.Width*g.Height*4)
	colors := [8][3]byte{{255, 255, 255}, {255, 255, 0}, {0, 255, 255}, {0, 255, 0}, {255, 0, 255}, {255, 0, 0}, {0, 0, 255}, {0, 0, 0}}
	for y := 0; y < g.Height; y++ {
		for x := 0; x < g.Width; x++ {
			c := colors[x*len(colors)/g.Width]
			if y >= g.Height*3/4 {
				v := byte(x * 255 / max(1, g.Width-1))
				c = [3]byte{v, v, v}
			}
			if x == 0 || y == 0 || x == g.Width-1 || y == g.Height-1 {
				c = [3]byte{255, 255, 255}
			}
			i := (y*g.Width + x) * 4
			pixels[i], pixels[i+1], pixels[i+2] = c[2], c[1], c[0]
		}
	}
	return d.Present(pixels)
}
