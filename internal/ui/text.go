package ui

import "strings"

// TextWidth returns the unscaled pixel width of the first line drawn by Text.
// Unicode variation selectors modify presentation and occupy no glyph cell.
func TextWidth(s string) int {
	width := 0
	for _, r := range s {
		if r == '\n' {
			break
		}
		if !isVariationSelector(r) {
			width += 8
		}
	}
	return width
}

// TruncateText fits one line into an unscaled pixel width. Truncated lines end
// with an ellipsis if more than three glyph cells fit. Widths below eight return
// an empty string. Variation selectors never consume space or split a glyph.
func TruncateText(s string, width int) string {
	s, _, _ = strings.Cut(s, "\n")
	n := max(0, width/8)
	if TextWidth(s) <= n*8 {
		return s
	}
	s = strings.Map(func(r rune) rune {
		if isVariationSelector(r) {
			return -1
		}
		return r
	}, s)
	r := []rune(s)
	if n > 3 {
		return string(r[:n-3]) + "..."
	}
	return string(r[:n])
}
