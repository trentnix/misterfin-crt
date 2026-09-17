// Package caption shapes and rasterizes plain Unicode subtitles and closed
// captions. It has no media-server, decoder, or display dependencies.
package caption

import (
	"image"
	"slices"
	"strings"

	"github.com/go-text/typesetting/di"
	"github.com/go-text/typesetting/shaping"
	"golang.org/x/image/math/fixed"
	"golang.org/x/text/unicode/bidi"
)

// Renderer lays out at most three centered lines in a 4:3 viewport. Its zero
// value is ready to use. Calls must be serial. Fonts load on demand, and the
// most recent cue image is cached until the text or viewport dimensions change.
type Renderer struct {
	fonts         fontSet
	segmenter     shaping.Segmenter
	shaper        shaping.HarfbuzzShaper
	wrapper       shaping.LineWrapper
	text          string
	width, height int
	cached        *image.NRGBA
}

// Image returns a borrowed, immutable image of outlined text with transparent
// surroundings, or nil for an empty cue. Width and height describe the logical
// viewport, whose pixels are stretched to a 4:3 display by the output adapter.
// Position is deliberately excluded from the cache so controls can move the
// same caption without reshaping it. Nonpositive dimensions return nil.
func (r *Renderer) Image(text string, width, height int) *image.NRGBA {
	if width <= 0 || height <= 0 {
		return nil
	}
	// Subtitle files already have this limit. Apply it to live captions and
	// other callers too, without allocating on repeated frames.
	count := 0
	for index := range text {
		if count == 2048 {
			text = text[:index]
			break
		}
		count++
	}
	if text == r.text && width == r.width && height == r.height {
		return r.cached
	}
	r.text, r.width, r.height = text, width, height
	r.cached = nil
	if strings.TrimSpace(text) == "" {
		return nil
	}
	r.fonts.init()
	size := max(12, width*22/640)
	lines := r.layout(text, max(1, width-width/10-8), size)
	r.cached = rasterize(lines, width-width/10, size, float32(height)*4/float32(width*3))
	return r.cached
}

// layout resolves paragraph direction, script, font fallback, and shaping before
// wrapping at Unicode line breaks. Overlong words can break at grapheme boundaries.
func (r *Renderer) layout(text string, width, size int) []shaping.Line {
	var lines []shaping.Line
	for _, paragraph := range strings.Split(text, "\n") {
		if strings.TrimSpace(paragraph) == "" {
			continue
		}
		runes := []rune(paragraph)
		direction := paragraphDirection(paragraph)
		input := shaping.Input{Text: runes, RunEnd: len(runes), Size: fixed.I(size), Direction: direction}
		segments := r.segmenter.Split(input, &r.fonts)
		runs := make([]shaping.Output, len(segments))
		for i, segment := range segments {
			runs[i] = r.shaper.Shape(segment)
		}
		config := shaping.WrapConfig{Direction: direction, TruncateAfterLines: 3 - len(lines)}
		config = config.WithTruncator(&r.shaper, shaping.Input{Text: []rune("…"), RunEnd: 1, Face: r.fonts.face("NotoSans-Regular.ttf"), Size: fixed.I(size), Direction: direction})
		wrapped, _ := r.wrapper.WrapParagraph(config, width, runes, shaping.NewSliceIterator(runs))
		for _, line := range wrapped {
			// The wrapper reuses its slices. Retain only this cue's shaped runs.
			line = slices.Clone(line)
			slices.SortFunc(line, func(a, b shaping.Output) int { return int(a.VisualIndex - b.VisualIndex) })
			lines = append(lines, line)
		}
		if len(lines) == 3 {
			break
		}
	}
	return lines
}

// paragraphDirection follows the first strong character, preserving the order
// of numbers and Latin words inside right-to-left paragraphs.
func paragraphDirection(text string) di.Direction {
	for _, ch := range text {
		properties, _ := bidi.LookupRune(ch)
		switch properties.Class() {
		case bidi.L:
			return di.DirectionLTR
		case bidi.R, bidi.AL:
			return di.DirectionRTL
		}
	}
	return di.DirectionLTR
}
