package ui

import (
	"bytes"
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

func TestOverlayShadeAndComposite(t *testing.T) {
	overlay := NewOverlay(1, 1)
	overlay.Shade(0, 0, 1, 1, 64)
	if !bytes.Equal(overlay.Pixels, []byte{0, 0, 0, 64}) {
		t.Fatalf("shade %v", overlay.Pixels)
	}
	frame := []byte{100, 120, 140, 0}
	Composite(frame, overlay.Pixels)
	if !bytes.Equal(frame, []byte{75, 90, 105, 0}) {
		t.Fatalf("composite %v", frame)
	}
	overlay.Rect(0, 0, 1, 1, 0x1e140a)
	if !bytes.Equal(overlay.Pixels, []byte{10, 20, 30, 255}) {
		t.Fatalf("opaque draw %v", overlay.Pixels)
	}
}

// Hiding the concrete type exercises the generic image path as a reference.
type genericImage struct{ image.Image }

func TestRGBABlitMatchesGeneric(t *testing.T) {
	parent := image.NewRGBA(image.Rect(3, 5, 40, 35))
	for y := 5; y < 35; y++ {
		for x := 3; x < 40; x++ {
			a := uint8((x*37 + y*13) % 256)
			parent.SetRGBA(x, y, color.RGBA{a / 2, a / 3, a / 4, a})
		}
	}
	source := parent.SubImage(image.Rect(7, 9, 33, 28)).(*image.RGBA)
	for _, box := range [][4]int{{0, 0, 32, 24}, {-7, -3, 40, 30}, {20, 10, 30, 25}, {50, 0, 10, 10}} {
		fast, generic := New(32, 24), New(32, 24)
		for i := range fast.Pixels {
			fast.Pixels[i] = byte(i)
			generic.Pixels[i] = byte(i)
		}
		fast.Blit(source, box[0], box[1], box[2], box[3])
		generic.Blit(genericImage{source}, box[0], box[1], box[2], box[3])
		if !bytes.Equal(fast.Pixels, generic.Pixels) {
			t.Fatalf("RGBA path changed pixels for box %v", box)
		}
	}
}

func TestOpaqueShadeMatchesChannelFormula(t *testing.T) {
	for _, alpha := range []int{0, 64, 145, 210, 255} {
		c := New(32, 24)
		for i := range c.Pixels {
			c.Pixels[i] = byte(i)
		}
		want := append([]byte(nil), c.Pixels...)
		for y := 0; y < 12; y++ {
			for x := 0; x < 20; x++ {
				for k := 0; k < 3; k++ {
					i := (y*32+x)*4 + k
					want[i] = byte(int(want[i]) * (255 - alpha) / 255)
				}
			}
		}
		c.Shade(-3, -2, 23, 14, alpha)
		if !bytes.Equal(c.Pixels, want) {
			t.Fatalf("shade changed pixels for alpha %d", alpha)
		}
	}
}
