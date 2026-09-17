package rendering

import "mistervision/internal/input/control"

// connectionChoices draws provider routes with the same safe margins and input
// badges as setup. Backend construction and persistence stay outside rendering.
func (p *screenPainter) connectionChoices() {
	a, c := p.scene.About, p.canvas
	c.Rect(0, 0, p.width, p.height, 0x0b0d13)
	title, choices := a.ConnectionChoices()
	center(c, p.safeY+4, truncate(title, p.width-48, 2), titleColor, 2)
	hints := []controlHint{}
	if len(choices) > 1 {
		hints = append(hints, pairedHint(p.scene.Controls, control.Up, control.Down, "Choose"))
	}
	action := "Select"
	if a.ConnectionSelected >= 0 && a.ConnectionSelected < len(choices) && choices[a.ConnectionSelected].Help != "" {
		action = "Setup help"
	}
	back := "Back"
	if p.scene.Setup.Kind != SetupHidden && len(a.ConnectionPath) == 0 && a.ConnectionMessage == "" && !a.CanReturnToConnection {
		back = "Exit"
	}
	hints = append(hints, hint(p.scene.Controls, control.Open, action), hint(p.scene.Controls, control.Back, back))
	rows := controlRows(p.width, hints)
	bottom := controlsTop(p.bottom, rows) - 12
	top := p.safeY + 34
	const rowHeight = 30
	count := max(1, (bottom-top-32)/rowHeight)
	selected := max(0, min(a.ConnectionSelected, len(choices)-1))
	start := max(0, min(selected-count/2, len(choices)-count))
	for i := start; i < min(len(choices), start+count); i++ {
		choice := choices[i]
		y := top + (i-start)*rowHeight
		color := uint32(0xcccccc)
		if i == selected {
			c.Rect(24, y-3, p.width-48, rowHeight-2, 0x283446)
			color = titleColor
		}
		name := choice.Name
		if choice.ID != "" && choice.ID == a.CurrentConnection {
			name += " (active)"
		}
		c.Text(32, y, truncate(name, p.width-64, 1), color, p.width-64)
		c.Text(32, y+12, truncate(choice.Description, p.width-64, 1), dimColor, p.width-64)
	}
	if a.ConnectionMessage != "" {
		setupLines(c, bottom-24, a.ConnectionMessage, 2)
	}
	drawControls(c, p.bottom, rows)
}
