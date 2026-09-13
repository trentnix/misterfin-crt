package browser

import (
	"misterfin-go/internal/input/control"
	"misterfin-go/internal/ui"
)

// controlHint pairs a physical input badge with a short action description.
// The renderer receives resolved names and never reads controller configuration.
type controlHint struct{ key, description string }

func hint(labels control.Labels, action, description string) controlHint {
	return controlHint{labels.Name(action), description}
}

func playbackHints(labels control.Labels, paused bool) []controlHint {
	action := "Pause"
	if paused {
		action = "Play"
	}
	return []controlHint{hint(labels, "open", action), hint(labels, "back", "Stop")}
}

const controlRowHeight = 20

func (h controlHint) width() int { return textWidth(h.key, 1) + 12 + 8 + textWidth(h.description, 1) }

// controlRows keeps related actions together and wraps long custom labels.
// Unbound actions disappear rather than advertising a key that cannot work.
func controlRows(width int, groups ...[]controlHint) [][]controlHint {
	var rows [][]controlHint
	for _, group := range groups {
		var row []controlHint
		used := 0
		for _, h := range group {
			if h.key == "" {
				continue
			}
			h.key = truncate(h.key, max(8, width-48-20-textWidth(h.description, 1)), 1)
			size := h.width()
			if len(row) > 0 && used+20+size > width-48 {
				rows = append(rows, row)
				row = nil
				used = 0
			}
			if len(row) > 0 {
				used += 20
			}
			row = append(row, h)
			used += size
		}
		if len(row) > 0 {
			rows = append(rows, row)
		}
	}
	return rows
}

// drawControls aligns badges and descriptions on each baseline. The final row
// sits at bottom, inside the same CRT safe area used by the rest of the UI.
func drawControls(c *ui.Canvas, bottom int, rows [][]controlHint) {
	for i, row := range rows {
		width := max(0, len(row)-1) * 20
		for _, h := range row {
			width += h.width()
		}
		x, y := (c.Width-width)/2, bottom-(len(rows)-1-i)*controlRowHeight
		for _, h := range row {
			badge := textWidth(h.key, 1) + 12
			c.Rect(x, y-3, badge, 14, 0x606060)
			c.Rect(x+1, y-2, badge-2, 12, 0x282828)
			c.Text(x+6, y, h.key, titleColor, c.Width-24)
			c.Text(x+badge+8, y, h.description, 0xd0d0d0, c.Width-24)
			x += h.width() + 20
		}
	}
}
