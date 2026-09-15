package rendering

import (
	"strings"

	"misterfin-crt/internal/branding"
	"misterfin-crt/internal/input/control"
	"misterfin-crt/internal/ui"
)

// setup draws connection progress, approval codes, and actionable setup errors.
// It receives typed state and resolved input labels, never raw errors or config.
func (p *screenPainter) setup() {
	s, c := p.scene.Setup, p.canvas
	hints := []controlHint{}
	if action := s.RetryLabel(); action != "" {
		hints = append(hints, hint(p.scene.Controls, control.Open, action))
	}
	hints = append(hints, hint(p.scene.Controls, control.Back, "Exit"))
	rows := controlRows(p.width, hints)
	bottom := controlsTop(p.bottom, rows) - 12
	top := p.safeY + 4
	logoHeight := min(48, max(16, bottom-top-132))
	p.cache.setup(c, top, logoHeight)
	heading, message := s.content()
	titleY := top + logoHeight + 8
	scale := 2
	if textWidth(heading, scale) > p.width-48 {
		scale = 1
	}
	center(c, titleY, truncate(heading, p.width-48, scale), titleColor, scale)
	bodyY := titleY + 28
	switch s.Kind {
	case SetupQuickConnect:
		setupLines(c, bodyY, message, 2)
		center(c, bottom-62, truncate(s.Code, p.width-48, 3), 0xffffff, 3)
		center(c, bottom-22, "Waiting for approval...", dimColor, 1)
		setupActivity(c, bottom-4, p.animation.Seconds)
	case SetupConnecting:
		setupLines(c, bodyY, message, 2)
		setupActivity(c, bottom-22, p.animation.Seconds)
	default:
		setupLines(c, bodyY, message, max(1, (bottom-42-bodyY)/12))
		if s.Path != "" {
			label := "Configuration file"
			if s.Kind == SetupSessionUnavailable {
				label = "Sign-in folder"
			}
			center(c, bottom-34, label, dimColor, 1)
			setupPath(c, bottom-20, s.Path)
		}
	}
	drawControls(c, p.bottom, rows)
}

// setupLines wraps short instructions within the shared CRT side margins.
func setupLines(c *ui.Canvas, y int, text string, rows int) {
	width := max(1, (c.Width-48)/8)
	for _, paragraph := range strings.Split(text, "\n") {
		line := ""
		for _, word := range strings.Fields(paragraph) {
			if line != "" && len([]rune(line+" "+word)) > width {
				if rows == 0 {
					return
				}
				center(c, y, line, 0xcccccc, 1)
				rows--
				y += 12
				line = ""
			}
			if line != "" {
				line += " "
			}
			line += word
		}
		if line != "" {
			if rows == 0 {
				return
			}
			center(c, y, truncate(line, c.Width-48, 1), 0xcccccc, 1)
			rows--
			y += 12
		}
	}

}

// setupPath wraps paths at character boundaries and preserves both ends when a
// path exceeds two lines. It displays the selected location, never its contents.
func setupPath(c *ui.Canvas, y int, path string) {
	width := max(8, (c.Width-48)/8)
	path = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return ' '
		}
		return r
	}, path)
	runes := []rune(path)
	if len(runes) > width*2 {
		runes = append(append(append([]rune{}, runes[:width-3]...), '.', '.', '.'), runes[len(runes)-width:]...)
	}
	for len(runes) > 0 {
		n := min(width, len(runes))
		center(c, y, string(runes[:n]), 0xcccccc, 1)
		runes = runes[n:]
		y += 12
	}
}

// setupActivity is a small indeterminate indicator. Only its highlighted block
// moves. The approval code and instructions remain still and readable.
func setupActivity(c *ui.Canvas, y int, seconds float64) {
	active := int(max(0, seconds)*4) % 6
	for i := 0; i < 6; i++ {
		color := uint32(0x303640)
		if i == active {
			color = titleColor
		}
		c.Rect(c.Width/2-28+i*10, y, 6, 3, color)
	}
}

// setup caches only the backdrop and scaled logo, never approval codes or text.
// Geometry and available logo height invalidate the prepared pixels.
func (s *sceneCache) setup(c *ui.Canvas, top, height int) {
	draw := func(dst *ui.Canvas) {
		dst.Rect(0, 0, dst.Width, dst.Height, 0x0b0d13)
		dst.Image(branding.Logo(), 24, top, dst.Width-48, height)
	}
	if s == nil {
		draw(c)
		return
	}
	if s.setupBase == nil || s.setupBase.Width != c.Width || s.setupBase.Height != c.Height || s.setupLogoHeight != height {
		s.setupBase = ui.New(c.Width, c.Height)
		s.setupLogoHeight = height
		draw(s.setupBase)
	}
	copy(c.Pixels, s.setupBase.Pixels)
}
