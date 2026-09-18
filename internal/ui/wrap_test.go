package ui

import (
	"strings"
	"testing"
)

func TestWrapTextKeepsAllGlyphsInsideWidth(t *testing.T) {
	for _, width := range []int{8, 40, 232, 552} {
		for _, text := range []string{"Could not load this library. Check the server and retry.", strings.Repeat("長", 50), "A\ufe0f B\ufe0f C\ufe0f"} {
			lines := WrapText(text, width)
			for _, line := range lines {
				if TextWidth(line) > width {
					t.Fatalf("%q exceeds %d", line, width)
				}
			}
			if strings.ReplaceAll(strings.Join(lines, ""), " ", "") != strings.ReplaceAll(text, " ", "") {
				t.Fatal("wrapping lost text")
			}
		}
	}
	if len(WrapText("text", 0)) != 0 {
		t.Fatal("zero-width layout returned text")
	}
}
