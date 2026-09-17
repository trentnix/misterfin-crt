package ui

import (
	"image"
	"image/color"
	"testing"
)

func TestOverlayStraightAlphaAndClipping(t *testing.T) {
	source := image.NewNRGBA(image.Rect(5, 7, 7, 9))
	for y := 7; y < 9; y++ {
		for x := 5; x < 7; x++ {
			source.SetNRGBA(x, y, color.NRGBA{R: 200, G: 100, B: 50, A: 128})
		}
	}
	for _, transparent := range []bool{false, true} {
		c := New(3, 3)
		if transparent {
			c = NewOverlay(3, 3)
		}
		c.Overlay(source, -1, -1)
		want := []byte{25, 50, 100, 0}
		if transparent {
			want = []byte{50, 100, 200, 128}
		}
		for i, value := range want {
			if c.Pixels[i] != value {
				t.Fatalf("transparent=%v: got %v want %v", transparent, c.Pixels[:4], want)
			}
		}
		for _, value := range c.Pixels[4:] {
			if value != 0 {
				t.Fatal("clipped pixels changed")
			}
		}
	}
}

func TestOverlayOverExistingTransparency(t *testing.T) {
	c := NewOverlay(1, 1)
	c.Shade(0, 0, 1, 1, 128)
	source := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	source.SetNRGBA(0, 0, color.NRGBA{R: 255, G: 255, B: 255, A: 128})
	c.Overlay(source, 0, 0)
	if c.Pixels[3] != 192 || c.Pixels[0] != 170 {
		t.Fatalf("straight-alpha composite = %v", c.Pixels)
	}
}
