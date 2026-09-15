package rendering

import (
	"math"
	"time"
)

// animationState owns elapsed time and motion for one renderer instance.
type animationState struct {
	initialized             bool
	start, last, titleSince time.Time
	title                   string
	value                   Animation
	identity                [4]string
	listTitle               string
	scroll                  float64
	scrollReady             bool
}

func (a *animationState) advance(scene Scene, rows int) Animation {
	now := scene.Now
	if !a.initialized {
		a.initialized = true
		a.start = now
		a.last = now
		a.titleSince = now
	}
	dt := max(0.0, min(now.Sub(a.last).Seconds(), 0.05))
	a.last = now
	a.value.Seconds = now.Sub(a.start).Seconds()
	if title := scene.title(); title != a.title {
		a.title, a.titleSince = title, now
	}
	a.value.TitleSeconds = now.Sub(a.titleSince).Seconds()
	a.value.Selection += (float64(scene.Content.Selected) - a.value.Selection) * (1 - math.Exp(-dt/0.035))
	row := float64(scene.Content.Selected - scene.Content.Scroll)
	if math.Abs(row-a.value.Row) > float64(rows) {
		a.value.Row = row
	} else {
		a.value.Row += (row - a.value.Row) * (1 - math.Exp(-dt/0.055))
	}
	target := float64(scene.Content.Start + scene.Content.Scroll)
	if !a.scrollReady || a.identity != scene.Content.Identity || a.listTitle != scene.Content.Title || math.Abs(target-a.scroll) > float64(rows) {
		a.scroll = target
	} else {
		a.scroll += (target - a.scroll) * (1 - math.Exp(-dt/0.055))
	}
	a.identity, a.listTitle, a.scrollReady = scene.Content.Identity, scene.Content.Title, true
	a.value.ScrollOffset = a.scroll - target
	return a.value
}

// Animation contains elapsed seconds and eased selection positions for a frame.
// Seconds drives background motion. TitleSeconds drives marquee scrolling.
// Selection and Row are fractional item and visible-row positions.
// ScrollOffset is the eased displacement from the target list scroll position.
type Animation struct{ Seconds, TitleSeconds, Selection, Row, ScrollOffset float64 }
