package caption

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/go-text/typesetting/di"
	"github.com/go-text/typesetting/language"
	"github.com/go-text/typesetting/shaping"
)

func TestBundledFonts(t *testing.T) {
	var fonts fontSet
	fonts.init()
	var manifest []struct{ File, SHA256 string }
	if err := json.Unmarshal(fonts.read("manifest.json"), &manifest); err != nil {
		t.Fatal(err)
	}
	for _, entry := range manifest {
		t.Run(entry.File, func(t *testing.T) {
			hash := sha256.Sum256(fonts.read(entry.File))
			if hex.EncodeToString(hash[:]) != entry.SHA256 {
				t.Fatal("font differs from recorded upstream source")
			}
			fonts.face(entry.File)
		})
	}
}

func TestMultilingualCoverage(t *testing.T) {
	var renderer Renderer
	renderer.fonts.init()
	for _, text := range []string{
		"††† Don’t go — déjà vu… ♪", "Ελληνικά", "Русские субтитры", "مرحبا بالعالم", "שלום עולם",
		"नमस्ते दुनिया", "বাংলা", "தமிழ்", "తెలుగు", "മലയാളം", "ಕನ್ನಡ", "ગુજરાતી", "ਪੰਜਾਬੀ", "සිංහල",
		"ภาษาไทย", "ພາສາລາວ", "ខ្មែរ", "မြန်မာ", "ქართული", "Հայերեն", "አማርኛ", "བོད་ཡིག",
		"日本語の字幕", "中文字幕", "한국어 자막",
	} {
		t.Run(text, func(t *testing.T) {
			lines := renderer.layout(text, 560, 22)
			if len(lines) == 0 {
				t.Fatal("no shaped text")
			}
			for _, line := range lines {
				for _, run := range line {
					for _, glyph := range run.Glyphs {
						if glyph.GlyphID == 0 {
							t.Fatalf("missing glyph for cluster %d", glyph.TextIndex())
						}
					}
				}
			}
			for _, height := range []int{240, 480} {
				img := renderer.Image(text, 640, height)
				if img == nil || !hasInk(img.Pix) {
					t.Fatal("empty raster")
				}
				if img.Bounds().Dx() > 576 || img.Bounds().Dy() > height/3 {
					t.Fatal("caption escaped safe region")
				}
			}
		})
	}
}

func hasInk(pixels []byte) bool {
	for i := 3; i < len(pixels); i += 4 {
		if pixels[i] != 0 {
			return true
		}
	}
	return false
}

func TestArabicJoiningAndIndicReordering(t *testing.T) {
	var renderer Renderer
	renderer.fonts.init()
	arabic := renderer.layout("لا", 560, 22)[0]
	if len(arabic) != 1 || arabic[0].Direction != di.DirectionRTL || len(arabic[0].Glyphs) != 1 {
		t.Fatalf("lam-alef was not joined: %+v", arabic)
	}
	indic := renderer.layout("कि", 560, 22)[0]
	if len(indic) != 1 || len(indic[0].Glyphs) < 2 {
		t.Fatalf("missing Devanagari shaping: %+v", indic)
	}
	nominal, _ := indic[0].Face.NominalGlyph('क')
	if indic[0].Glyphs[0].GlyphID == nominal {
		t.Fatal("pre-base vowel was not reordered")
	}
}

func TestBidirectionalOrder(t *testing.T) {
	var renderer Renderer
	renderer.fonts.init()
	text := "שלום 123"
	line := renderer.layout(text, 560, 22)[0]
	if line[0].Direction != di.DirectionLTR || string([]rune(text)[line[0].Runes.Offset:line[0].Runes.Offset+line[0].Runes.Count]) != "123" {
		t.Fatalf("digits should be leftmost and retain LTR order: %+v", line)
	}
	last := line[len(line)-1]
	if last.Direction != di.DirectionRTL || last.Glyphs[0].TextIndex() <= last.Glyphs[len(last.Glyphs)-1].TextIndex() {
		t.Fatal("Hebrew glyphs did not retain RTL visual order")
	}
}

func TestCombiningMarks(t *testing.T) {
	var renderer Renderer
	composed := bytes.Clone(renderer.Image("Café", 640, 240).Pix)
	decomposed := renderer.Image("Cafe\u0301", 640, 240).Pix
	if !bytes.Equal(composed, decomposed) {
		t.Fatal("canonical combining sequence differs from composed text")
	}
}

func TestUnicodeWrappingAndBounds(t *testing.T) {
	var renderer Renderer
	renderer.fonts.init()
	for _, text := range []string{strings.Repeat("日本語の字幕", 30), strings.Repeat("مرحبا ", 80), strings.Repeat("a\u0301", 300)} {
		lines := renderer.layout(text, 160, 22)
		if len(lines) != 3 {
			t.Fatalf("got %d lines, want three", len(lines))
		}
		for _, line := range lines {
			var width int
			for _, run := range line {
				width += run.Advance.Ceil()
			}
			if width > 162 {
				t.Fatalf("line width %d exceeds wrapping width", width)
			}
		}
	}
	renderer.Image(strings.Repeat("字", 4096), 640, 240)
	if len([]rune(renderer.text)) != 2048 {
		t.Fatal("cue length was not bounded")
	}
}

func TestCueCacheAndLazyFonts(t *testing.T) {
	var renderer Renderer
	first := renderer.Image("Hello", 640, 240)
	if len(renderer.fonts.faces) != 1 {
		t.Fatal("Latin cue loaded unnecessary fonts")
	}
	if allocs := testing.AllocsPerRun(100, func() {
		if renderer.Image("Hello", 640, 240) != first {
			t.Fatal("unchanged cue was rasterized again")
		}
	}); allocs != 0 {
		t.Fatalf("cached cue allocated %g times", allocs)
	}
	if renderer.Image("Hello", 640, 480) == first {
		t.Fatal("size change did not invalidate cache")
	}
	if renderer.Image("Goodbye", 640, 480) == first {
		t.Fatal("text change did not invalidate cache")
	}
	if renderer.Image("", 640, 480) != nil {
		t.Fatal("empty cue retained old pixels")
	}
	if renderer.Image("   \n", 640, 240) != nil {
		t.Fatal("blank cue drew pixels")
	}
	if renderer.Image("Hello", 0, 240) != nil {
		t.Fatal("invalid viewport drew pixels")
	}
}

func TestMissingGlyphRemainsVisible(t *testing.T) {
	var renderer Renderer
	renderer.fonts.init()
	renderer.fonts.SetScript(language.Unknown)
	face := renderer.fonts.ResolveFace('\U0010ffff')
	if glyph, _ := face.NominalGlyph('\U0010ffff'); glyph != 0 {
		t.Fatal("test rune unexpectedly supported")
	}
	if !hasInk(renderer.Image("\U0010ffff", 640, 240).Pix) {
		t.Fatal("unsupported character disappeared")
	}
}

func BenchmarkCaption(b *testing.B) {
	for _, text := range []string{"The quick brown fox jumps over the lazy dog.", "日本語の字幕を表示します。", "مرحبا بالعالم 123"} {
		b.Run(text, func(b *testing.B) {
			var renderer Renderer
			renderer.Image(text, 640, 240)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				renderer.Image(text, 640, 240)
			}
		})
	}
}

func BenchmarkCaptionChange(b *testing.B) {
	for _, text := range []string{"The quick brown fox jumps over the lazy dog.", "日本語の字幕を表示します。", "مرحبا بالعالم 123"} {
		b.Run(text, func(b *testing.B) {
			var renderer Renderer
			renderer.Image(text, 640, 240)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				renderer.Image(fmt.Sprintf("%s %d", text, i), 640, 240)
			}
		})
	}
}

// Verify the fallback contract remains explicit as the shaping library evolves.
var _ shaping.FontmapScript = (*fontSet)(nil)

func BenchmarkCaptionCold(b *testing.B) {
	for _, text := range []string{"Hello", "日本語の字幕"} {
		b.Run(text, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				var renderer Renderer
				renderer.Image(text, 640, 240)
			}
		})
	}
}
