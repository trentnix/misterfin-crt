package ui

import (
	_ "embed"
	"encoding/binary"
	"sort"
)

// unicodeFont is CRT Unicode Fallback, derived from Fusion Pixel Font 8px.
// Each sorted record contains a little-endian code point and eight bitmap rows.
// See docs/licenses/fusion-pixel.txt and tools/build_unicode_font.py.
//
//go:embed fonts/unicode.bin
var unicodeFont []byte

// glyphForRune retains the original Latin-1 font and looks up other characters
// in the embedded bitmap fallback. Unsupported scripts still use a question mark.
// Lookup allocates no memory and needs no font engine or mutable glyph cache.
func glyphForRune(r rune) [8]byte {
	if r < 32 {
		return font[' ']
	}
	if r < 256 {
		return font[r]
	}
	const recordSize = 12
	n := len(unicodeFont) / recordSize
	i := sort.Search(n, func(i int) bool { return binary.LittleEndian.Uint32(unicodeFont[i*recordSize:]) >= uint32(r) })
	if i < n && binary.LittleEndian.Uint32(unicodeFont[i*recordSize:]) == uint32(r) {
		return [8]byte(unicodeFont[i*recordSize+4 : i*recordSize+recordSize])
	}
	return font['?']
}
