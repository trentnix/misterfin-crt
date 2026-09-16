package ui

import (
	"bytes"
	"encoding/binary"
	"testing"
	"unicode/utf8"
)

func TestUnicodeFallback(t *testing.T) {
	for _, r := range "†‡‘’“”‐–—…♪♫ō小圈子愛の惑星암호" {
		glyph := glyphForRune(r)
		if glyph == font['?'] || glyph == ([8]byte{}) {
			t.Errorf("missing glyph U+%04X", r)
		}
	}
	for r := rune(32); r < 256; r++ {
		if glyphForRune(r) != font[r] {
			t.Fatalf("Latin-1 changed at U+%04X", r)
		}
	}
	if glyphForRune(utf8.MaxRune) != font['?'] {
		t.Fatal("unsupported rune has no fallback")
	}
}

func TestUnicodeFontRecords(t *testing.T) {
	if len(unicodeFont)%12 != 0 || len(unicodeFont) == 0 {
		t.Fatal("invalid font records")
	}
	last := uint32(255)
	for i := 0; i < len(unicodeFont); i += 12 {
		code := binary.LittleEndian.Uint32(unicodeFont[i:])
		if code <= last || !utf8.ValidRune(rune(code)) {
			t.Fatalf("invalid or unsorted code point %d", code)
		}
		last = code
	}
}

func TestUnicodeTextScalingAndClipping(t *testing.T) {
	for _, scale := range []int{1, 2} {
		c := New(24*scale, 8*scale)
		c.TextScaled(0, 0, "†††", 0xffffff, 16*scale, scale)
		q := New(c.Width, c.Height)
		q.TextScaled(0, 0, "??", 0xffffff, 16*scale, scale)
		if bytes.Equal(c.Pixels, q.Pixels) {
			t.Fatal("Crosses rendered as question marks")
		}
		for y := 0; y < c.Height; y++ {
			for x := 16 * scale; x < c.Width; x++ {
				if !bytes.Equal(c.Pixels[(y*c.Width+x)*4:(y*c.Width+x+1)*4], []byte{0, 0, 0, 0}) {
					t.Fatal("glyph exceeded text right edge")
				}
			}
		}
	}
	if allocs := testing.AllocsPerRun(100, func() { _ = glyphForRune('†') }); allocs != 0 {
		t.Fatalf("glyph lookup allocates: %g", allocs)
	}
}
