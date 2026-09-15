package native

import (
	"testing"

	"misterfin-crt/internal/platform"
)

func TestInterlacedOverlayUsesFullFrame(t *testing.T) {
	for _, height := range []int{480, 576} {
		g := platform.Geometry{Width: 640, Height: height / 2, OutputWidth: 640, OutputHeight: height}
		pixels := make([]byte, g.Width*g.Height*4)
		// Different rows expose offsets, skipped rows, and uneven scaling.
		for y := 0; y < g.Height; y++ {
			for x := 0; x < g.Width; x++ {
				offset := (y*g.Width + x) * 4
				pixels[offset] = byte(y)
				pixels[offset+3] = 255
			}
		}
		scaled := scale(pixels, g)
		x, y, w, h := bounds(scaled, 640, height)
		if x != 0 || y != 0 || w != 640 || h != height {
			t.Fatalf("overlay bounds: %d,%d %dx%d", x, y, w, h)
		}
		for y := 0; y < height; y++ {
			if got := scaled[(y*640+320)*4]; got != byte(y/2) {
				t.Fatalf("row %d: got %d, want %d", y, got, byte(y/2))
			}
		}
	}
}
