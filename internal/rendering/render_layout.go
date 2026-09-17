package rendering

import (
	"fmt"

	"misterfin-crt/internal/input/control"
	"misterfin-crt/internal/musicviz"
	"misterfin-crt/internal/ui"
)

const titleColor = 0xffe040
const dimColor = 0x808080

func safeY(w, h int) int { return int(24*float64(h*4)/float64(w*3) + 0.5) }

// VisibleRows returns the list capacity within the CRT safe area for positive logical dimensions.
// Navigation must use this capacity when centering a selection or retaining pages.
func VisibleRows(w, h int) int { return max(1, (h-2*safeY(w, h)-32)/30) }

func textWidth(s string, scale int) int { return ui.TextWidth(s) * scale }

func truncate(s string, width, scale int) string {
	return ui.TruncateText(s, width/scale)
}

func center(c *ui.Canvas, y int, s string, color uint32, scale int) {
	c.TextScaled((c.Width-textWidth(s, scale))/2, y, s, color, c.Width, scale)
}

func runtime(ticks int64) string {
	seconds := max(int64(0), ticks/10000000)
	if seconds >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", seconds/3600, seconds/60%60, seconds%60)
	}
	return fmt.Sprintf("%d:%02d", seconds/60, seconds%60)
}

// screenPainter borrows one frame's canvas, scene, and renderer-owned cache.
// It computes shared CRT layout once and dispatches drawing without owning
// persistent state. Methods run synchronously during Render.
type screenPainter struct {
	visualizer                   *musicviz.Renderer
	canvas                       *ui.Canvas
	cache                        *sceneCache
	scene                        Scene
	animation                    Animation
	width, height, safeY, bottom int
}

// clock paints the shared local-time display in the top safe area.
func (p *screenPainter) clock() {
	p.canvas.Text(p.width-72, p.safeY+4, p.scene.Now.Format("15:04"), dimColor, p.width-32)
}

// header truncates the root heading and scrolls longer library or item titles.
// Both stay inside the safe area reserved beside the clock.
func (p *screenPainter) header(title string, titleY int) {
	c, w, h, sy, anim := p.canvas, p.width, p.height, titleY, p.animation

	end := w - 84
	if p.scene.Root {
		title = truncate(title, end-24, 2)
	}
	x := 24
	if textWidth(title, 2) > end-x {
		x -= int(anim.TitleSeconds*15) % (textWidth(title, 2) + 40)
	}
	// Clip the marquee to the title's safe area, including its repeated copy.
	layer := ui.New(w, 16)
	layer.TextScaled(x, 0, title, titleColor, end, 2)
	if x < 24 {
		layer.TextScaled(x+textWidth(title, 2)+40, 0, title, titleColor, end, 2)
	}
	for y := sy; y < min(h, sy+16); y++ {
		for x := 24; x < end; x++ {
			i := (y*w + x) * 4
			j := ((y-sy)*w + x) * 4
			if layer.Pixels[j]|layer.Pixels[j+1]|layer.Pixels[j+2] != 0 {
				copy(c.Pixels[i:i+3], layer.Pixels[j:j+3])
			}
		}
	}
	p.clock()
}

// footer draws browsing hints, request errors, and modal notices in that order.
func (p *screenPainter) footer(controls [][]controlHint) {
	c := p.canvas
	w, h := p.width, p.height
	bottom := p.bottom
	s := p.scene
	messageY := bottom - 14
	if len(controls) > 0 {
		drawControls(c, bottom, controls)
		messageY = controlsTop(bottom, controls) - 12
	}
	messageWidth := w - 48
	v := &s.Content
	if v.Detail == nil && (!s.Root || s.ListMode) && v.Page.TotalRecordCount != nil && *v.Page.TotalRecordCount > VisibleRows(w, h) {
		count := v.Count()
		countWidth := textWidth(count, 1)
		c.Text(w-24-countWidth, messageY, count, dimColor, w-24)
		messageWidth -= countWidth + 16
	}
	message := p.footerMessage()
	if message != "" {
		message = truncate(message, messageWidth, 1)
		c.Text(24+(messageWidth-textWidth(message, 1))/2, messageY, message, 0xff6060, w-24)
	}
	if s.ExitConfirm {
		rows := controlRows(w, []controlHint{hint(s.Controls, control.Open, "Exit"), hint(s.Controls, control.Back, "Cancel")})
		height := 28 + max(1, len(rows))*controlRowHeight
		top := (h - height) / 2
		c.Rect(12, top, w-24, height, 0x101010)
		center(c, top+6, "Exit?", titleColor, 2)
		drawControls(c, top+30+max(0, len(rows)-1)*controlRowHeight+controlBottomInset, rows)
	} else if s.Notice != "" {
		message := s.Notice
		scale := 1
		width := textWidth(message, scale)
		c.Shade((w-width)/2-12, h/2-8*scale-12, width+24, 16*scale+24, 210)
		center(c, h/2-4*scale, message, titleColor, scale)
	}
}

// footerMessage supplies the status text that browsing layouts reserve room for.
func (p *screenPainter) footerMessage() string {
	v := &p.scene.Content
	if v.Error != "" {
		return v.Error
	}
	if v.Loading || (v.Fetching && v.Scroll+VisibleRows(p.width, p.height) > len(v.Page.Items)) {
		return "Loading..."
	}
	return p.scene.SelectionError
}
