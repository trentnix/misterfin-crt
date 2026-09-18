package rendering

import (
	"fmt"

	"mistervision/internal/branding"
	"mistervision/internal/connection"
	"mistervision/internal/input/control"
	"mistervision/internal/ui"
	"mistervision/internal/update"
)

// about draws project identity and release state using the shared raster path.
// Controls use the same binding labels and safe margins as browsing screens.
func (p *screenPainter) about() {
	a := p.scene.About
	if a.ConnectionsVisible {
		p.connectionChoices()
		return
	}
	if a.NotesVisible {
		p.releaseNotes()
		return
	}
	hints := aboutHints(a, p.scene.Controls, p.scene.Setup.Kind == SetupHidden, false)
	rows := controlRows(p.width, hints)
	lines := messageLines(a.Status(), p.width-48, 6)
	statusY := controlsTop(p.bottom, rows) - 18 - max(0, len(lines)-1)*10
	baseY := statusY
	if a.Profile != nil {
		baseY -= 14
	}
	// Keep a useful logo area above the heading. Narrow screens also need
	// wrapped attribution, so use a small side-by-side logo and identity block.
	compact := baseY-86 < p.safeY+36 || textWidth(aboutLicense, 1) > p.width-48
	if compact {
		rows = controlRows(p.width, aboutHints(a, p.scene.Controls, p.scene.Setup.Kind == SetupHidden, true))
		statusY = controlsTop(p.bottom, rows) - 18 - max(0, len(lines)-1)*10
		baseY = statusY
		if a.Profile != nil {
			baseY -= 14
		}
	}
	p.cache.about(p.canvas, baseY, compact)
	if a.Profile != nil && (!compact || baseY >= p.safeY+32) {
		name := truncate(a.Profile.Name, p.width-48-profileLabelInset, 1)
		drawProfileLabel(p.canvas, (p.width-textWidth(name, 1)-profileLabelInset)/2, statusY-14, name, a.Profile.Avatar, titleColor)
	}
	if compact {
		p.canvas.Text(104, p.safeY+16, truncate("Version "+a.Build.String(), p.width-128, 1), dimColor, p.width-24)
	} else {
		center(p.canvas, baseY-62, truncate("Version "+a.Build.String(), p.width-48, 1), dimColor, 1)
	}
	color := uint32(0xc0c0c0)
	if a.Release.Available && !a.Checking && a.Message == "" {
		color = titleColor
	}
	for i, line := range lines {
		center(p.canvas, statusY+i*10, line, color, 1)
	}
	drawControls(p.canvas, p.bottom, rows)
}

// aboutHints keeps every action available when a compact layout uses shorter labels.
func aboutHints(a AboutPresentation, labels control.Labels, setupHidden, compact bool) []controlHint {
	profileLabel, releaseLabel, updateLabel := "Switch profile", "View release", "Check updates"
	if compact {
		profileLabel, releaseLabel, updateLabel = "Profile", "Release", "Updates"
	}
	var hints []controlHint
	if a.ProfileAction == connection.ProfileAdd {
		profileLabel = "Add user"
	}
	if a.ProfileAction != connection.ProfileUnchanged && setupHidden {
		hints = append(hints, hint(labels, control.Up, profileLabel))
	}
	if len(a.Connections) > 0 {
		hints = append(hints, hint(labels, control.Down, "Connections"))
	}
	if a.ForgetLabel != "" && setupHidden {
		label := a.ForgetLabel
		if compact && label == "Forget user" {
			label = "Forget"
		}
		hints = append(hints, hint(labels, control.Next, label))
	}
	if a.Release.Available && !a.Checking {
		hints = append(hints, hint(labels, control.Open, releaseLabel))
	}
	if !a.Checking {
		hints = append(hints, hint(labels, control.Select, updateLabel))
	}
	hints = append(hints, hint(labels, control.Back, "Back"))
	return hints
}

const aboutLicense = "CC BY-NC 4.0. Components have separate licenses."

// about caches the static logo and attribution at the current geometry. Source
// artwork is embedded and decoded once. Repeated draws only copy prepared pixels.
func (s *sceneCache) about(c *ui.Canvas, statusY int, compact bool) {
	draw := func(dst *ui.Canvas) {
		dst.Rect(0, 0, dst.Width, dst.Height, 0x0b0d13)
		top := safeY(dst.Width, dst.Height) + 4
		if compact {
			dst.Image(branding.Logo(), 24, top, 72, 24)
			dst.Text(104, top, "MiSTerVision", titleColor, dst.Width-24)
			var credits []string
			for _, text := range []string{"Trent Nix", "Based on MiSTerFin by Pudding Studio", aboutLicense} {
				credits = append(credits, ui.WrapText(text, dst.Width-48)...)
			}
			// With unusually long control labels, recovery guidance takes priority.
			// Never let attribution collide with the status or identity header.
			y := top + 28
			if y+len(credits)*10 <= statusY-2 {
				for _, line := range credits {
					center(dst, y, line, dimColor, 1)
					y += 10
				}
			}
			return
		}
		titleY := statusY - 86
		dst.Image(branding.Logo(), 24, top, dst.Width-48, max(1, titleY-top-8))
		center(dst, titleY, "MiSTerVision", titleColor, 2)
		center(dst, statusY-44, "Trent Nix", 0xc0c0c0, 1)
		center(dst, statusY-32, "Based on MiSTerFin by Pudding Studio", dimColor, 1)
		center(dst, statusY-20, aboutLicense, dimColor, 1)
	}
	if s == nil {
		draw(c)
		return
	}
	if s.aboutBase == nil || s.aboutBase.Width != c.Width || s.aboutBase.Height != c.Height || s.aboutStatusY != statusY || s.aboutCompact != compact {
		s.aboutBase = ui.New(c.Width, c.Height)
		s.aboutStatusY = statusY
		s.aboutCompact = compact
		draw(s.aboutBase)
	}
	copy(c.Pixels, s.aboutBase.Pixels)
}

// releaseNotes keeps confirmation, progress, and scrolling in the shared UI.
func (p *screenPainter) releaseNotes() {
	a := p.scene.About
	c := p.canvas
	c.Rect(0, 0, p.width, c.Height, 0x0b0d13)
	center(c, p.safeY+4, truncate("Release "+a.Release.Latest, p.width-48, 2), titleColor, 2)
	layout := a.notesLayout(p.width, p.height, p.scene.Controls)
	rows, statusY, top, count := layout.controls, layout.statusY, layout.top, layout.rows
	start := min(a.Scroll, max(0, len(a.Notes)-count))
	for index := start; index < min(len(a.Notes), start+count); index++ {
		c.Text(24, top+(index-start)*12, a.Notes[index], 0xcccccc, p.width-24)
	}
	if len(a.Notes) > count {
		center(c, statusY-12, fmt.Sprintf("%d-%d of %d", start+1, min(start+count, len(a.Notes)), len(a.Notes)), dimColor, 1)
	}
	for i, line := range messageLines(a.Status(), p.width-48, 6) {
		center(c, statusY+i*10, line, titleColor, 1)
	}
	if a.Updating {
		setupActivity(c, statusY+12, p.animation.Seconds)
	}
	drawControls(c, p.bottom, rows)
}

type notesLayout struct {
	controls           [][]controlHint
	statusY, top, rows int
}

func (a AboutPresentation) notesLayout(width, height int, labels control.Labels) notesLayout {
	var hints []controlHint
	switch {
	case a.Installed:
	case a.Updating:
		if a.Progress.Phase != update.Installing {
			hints = append(hints, hint(labels, control.Back, "Cancel"))
		}
	default:
		hints = append(hints, pairedHint(labels, control.Up, control.Down, "Scroll"))
		if a.CanInstall && !a.ManualInstall && a.Release.HasBundle {
			hints = append(hints, hint(labels, control.Open, "Install"))
		}
		hints = append(hints, hint(labels, control.Back, "Back"))
	}
	rows := controlRows(width, hints)
	statusY := controlsTop(height-8-safeY(width, height), rows) - 20 - max(0, len(messageLines(a.Status(), width-48, 6))-1)*10
	top := safeY(width, height) + 34
	count := max(1, (statusY-top-12)/12)
	return notesLayout{controls: rows, statusY: statusY, top: top, rows: count}
}

// ScrollLimit shares the renderer's visible-row calculation with input handling,
// including extra footer rows needed by long configured button labels.
func (a AboutPresentation) ScrollLimit(width, height int, labels control.Labels) int {
	return max(0, len(a.Notes)-a.notesLayout(width, height, labels).rows)
}
