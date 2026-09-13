package browser

import (
	"fmt"

	"misterfin-go/internal/musicviz"
	"misterfin-go/internal/ui"
)

const titleColor = 0xffe040
const dimColor = 0x808080

func safeY(w, h int) int { return int(24*float64(h*4)/float64(w*3) + 0.5) }

func visibleRows(w, h int) int { return max(1, (h-2*safeY(w, h)-32)/30) }

func textWidth(s string, scale int) int { return len([]rune(s)) * 8 * scale }

func truncate(s string, width, scale int) string {
	r := []rune(s)
	n := max(0, width/(8*scale))
	if len(r) <= n {
		return s
	}
	if n > 3 {
		return string(r[:n-3]) + "..."
	}
	return string(r[:n])
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

// header draws the scrolling title, clips both copies to the safe area, and
// paints the clock. The marquee scratch layer preserves existing pixel output.
func (p *screenPainter) header(title string) {
	c, w, h, sy, anim := p.canvas, p.width, p.height, p.safeY, p.animation

	end := w - 84
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
func (p *screenPainter) footer(hint string) {
	c := p.canvas
	selectionError := p.scene.SelectionError
	w, h := p.width, p.height
	bottom := p.bottom
	v := &p.scene.View
	s := p.scene
	center(c, bottom, hint, dimColor, 1)
	message := selectionError
	if v.Loading || (v.fetching && v.Scroll+visibleRows(w, h) > len(v.Page.Items)) {
		message = "Loading..."
	}
	if v.Error != "" {
		message = v.Error + "  R:retry"
	}
	if message != "" {
		center(c, bottom-14, truncate(message, w-48, 1), 0xff6060, 1)
	}
	if s.ExitConfirm || s.Notice != "" {
		message := s.Notice
		scale := 1
		if s.ExitConfirm {
			message = "Exit? [B: yes  A: no]"
			scale = 2
		}
		width := textWidth(message, scale)
		c.Shade((w-width)/2-12, h/2-8*scale-12, width+24, 16*scale+24, 210)
		center(c, h/2-4*scale, message, titleColor, scale)
	}
}
