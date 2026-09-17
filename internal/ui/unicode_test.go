package ui

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestUnicodeFallback(t *testing.T) {
	for _, r := range "†‡‘’“”‐–—…♪♫♥❤ō小圈子愛の惑星암호" {
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

func TestVariationSelectorsMatchMonochromeText(t *testing.T) {
	for _, text := range []string{"❤️ Tracks", "Fresh ❤️", "❤︎ Tracks", "愛\U000e0100"} {
		plain := strings.Map(func(r rune) rune {
			if r == '\ufe0f' || r == '\ufe0e' || r == '\U000e0100' {
				return -1
			}
			return r
		}, text)
		for _, scale := range []int{1, 2} {
			for _, width := range []int{8, 32, 80} {
				got, want := New(160, 16), New(160, 16)
				got.TextScaled(0, 0, text, 0xffffff, width*scale, scale)
				want.TextScaled(0, 0, plain, 0xffffff, width*scale, scale)
				if !bytes.Equal(got.Pixels, want.Pixels) {
					t.Errorf("%q scale=%d width=%d: presentation modifier changed drawing", text, scale, width)
				}
			}
		}
		if got, want := TextWidth(text), utf8.RuneCountInString(plain)*8; got != want {
			t.Errorf("%q: width=%d want=%d", text, got, want)
		}
		got, want := New(64, 40), New(64, 40)
		got.Wrap(0, 0, 64, 4, text+" next", 0xffffff)
		want.Wrap(0, 0, 64, 4, plain+" next", 0xffffff)
		if !bytes.Equal(got.Pixels, want.Pixels) {
			t.Errorf("%q: presentation modifier changed wrapping", text)
		}
	}
}

func TestTruncateTextUsesVisibleGlyphs(t *testing.T) {
	for _, tc := range []struct {
		text  string
		width int
		want  string
	}{
		{"❤️ Tracks", 64, "❤️ Tracks"},
		{"❤️ Tracks", 40, "❤ ..."},
		{"Fresh ❤️", 56, "Fresh ❤️"},
		{"❤️ Tracks", 8, "❤"},
		{"❤️ Tracks", 0, ""},
		{"❤️ Tracks", -8, ""},
		{"❤️\nTracks", 8, "❤️"},
	} {
		if got := TruncateText(tc.text, tc.width); got != tc.want {
			t.Errorf("TruncateText(%q, %d)=%q want=%q", tc.text, tc.width, got, tc.want)
		}
	}
}
