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

// WrapText returns word-wrapped lines that fit width pixels. It collapses
// whitespace and splits long words without separating variation selectors from
// their preceding glyph. Widths below one glyph return no lines.
func WrapText(s string, width int) []string {
	if width < 8 {
		return nil
	}
	var lines []string
	line := ""
	for _, word := range strings.Fields(s) {
		if line != "" && TextWidth(line+" "+word) <= width {
			line += " " + word
			continue
		}
		if line != "" {
			lines = append(lines, line)
			line = ""
		}
		for _, r := range word {
			if !isVariationSelector(r) && TextWidth(line)+8 > width {
				lines = append(lines, line)
				line = ""
			}
			line += string(r)
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}
