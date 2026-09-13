package browser

import (
	"strings"
)

// status draws connection progress, Quick Connect, or connection errors.
func (p *screenPainter) status() {
	c := p.canvas
	status := p.scene.Status
	w, h := p.width, p.height
	bottom := p.bottom

	heading := "MiSTerFin-Go"
	if strings.HasPrefix(status, "Quick Connect: ") {
		heading = "Quick Connect"
	} else if status != "Connecting to Jellyfin..." {
		heading = "Can't connect to server"
	}
	center(c, h/2-44, heading, titleColor, 2)
	if strings.HasPrefix(status, "Quick Connect: ") {
		code := strings.Split(strings.TrimPrefix(status, "Quick Connect: "), "\n")[0]
		center(c, h/2-12, "Enter this code in your Jellyfin client:", dimColor, 1)
		center(c, h/2+8, code, 0xffffff, 3)
		center(c, h/2+48, "Waiting for sign-in...", 0xcccccc, 1)
	} else {
		c.Wrap(24, h/2, w-48, 6, status, 0xcccccc)
	}
	center(c, bottom, "B:try again   A:exit", dimColor, 1)
}
