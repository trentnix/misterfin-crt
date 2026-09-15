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
		scaled := scale(nil, pixels, g)
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

func TestScaleClearsReusedMargins(t *testing.T) {
	// Equal buffer sizes with different aspect ratios exercise reuse across geometry.
	g := platform.Geometry{Width: 4, Height: 2, OutputWidth: 8, OutputHeight: 2}
	previous := make([]byte, 8*2*4)
	for i := range previous {
		previous[i] = 255
	}
	source := make([]byte, 4*2*4)
	for i := range source {
		source[i] = 80
	}
	got := scale(previous, source, g)
	for y := 0; y < 2; y++ {
		for x := 0; x < 8; x++ {
			want := byte(0)
			if x >= 3 && x < 5 {
				want = 80
			}
			for channel := 0; channel < 4; channel++ {
				if got[(y*8+x)*4+channel] != want {
					t.Fatalf("stale pixel at %d,%d", x, y)
				}
			}
		}
	}
}
