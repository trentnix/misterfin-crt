package ui

import (
	"image"
	"image/color"
	"testing"
)

func TestArtworkPreservesPhysicalAspect(t *testing.T) {
	c := New(640, 240)
	im := image.NewRGBA(image.Rect(0, 0, 10, 10))
	for y := 0; y < 10; y++ {
		for x := 0; x < 10; x++ {
			im.Set(x, y, color.RGBA{R: 255, A: 255})
		}
	}
	c.Image(im, 0, 0, 100, 100)
	// NTSC logical pixels have physical aspect 1:2. A square must occupy
	// 100 logical columns and 50 logical rows, centered in the allotted area.
	for _, p := range []struct {
		x, y int
		red  byte
	}{{0, 24, 0}, {0, 25, 255}, {99, 74, 255}, {99, 75, 0}, {100, 25, 0}} {
		if got := c.Pixels[(p.y*640+p.x)*4+2]; got != p.red {
			t.Errorf("pixel %d,%d: %d", p.x, p.y, got)
		}
	}
}

func TestTextClipsAndSupportsLatin1(t *testing.T) {
	c := New(40, 16)
	c.Text(-4, -2, "Amélie", 0xffffff, 40)
	if len(c.Pixels) != 40*16*4 {
		t.Fatal("canvas changed size")
	}
	c = New(8, 8)
	c.Text(0, 0, "é", 0xffffff, 8)
	nonzero := false
	for _, b := range c.Pixels {
		nonzero = nonzero || b != 0
	}
	if !nonzero {
		t.Fatal("Latin-1 glyph missing")
	}
}

func TestTransparentArtworkCompositesOverBackground(t *testing.T) {
	c := New(2, 2)
	c.Rect(0, 0, 2, 2, 0x0000ff)
	im := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	im.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 128})
	c.Blit(im, 0, 0, 2, 2)
	if c.Pixels[0] < 126 || c.Pixels[0] > 127 || c.Pixels[2] != 128 {
		t.Fatalf("blend: %v", c.Pixels[:4])
	}
	if c.Pixels[4] != 255 || c.Pixels[6] != 0 {
		t.Fatal("transparent pixel erased background")
	}
}
