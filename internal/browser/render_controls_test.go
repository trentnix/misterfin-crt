package browser

import (
	"bytes"
	"testing"
	"time"

	"misterfin-go/internal/input/control"
	"misterfin-go/internal/ui"
)

func TestControlLabelsReachSharedVideoRenderer(t *testing.T) {
	p := PlaybackPresentation{Active: true, ControlsVisible: true, Seekable: true, Title: "Episode"}
	s := Scene{Video: true, Playback: p, Now: time.Unix(100, 0), Controls: control.KeyboardLabels()}
	r := NewRenderer()
	keyboard := append([]byte(nil), r.Render(640, 240, s).Overlay...)
	expected := renderVideoOverlayOn(ui.NewOverlay(640, 240), p, s.Now, s.Controls)
	if !bytes.Equal(keyboard, expected) {
		t.Fatal("scene labels did not reach video overlay")
	}
	s.Controls = control.Labels{"seek-backward": "L1", "seek-forward": "R1", "open": "Cross", "back": "Circle", "up": "Hat up"}
	if bytes.Equal(keyboard, r.Render(640, 240, s).Overlay) {
		t.Fatal("controller kept keyboard instructions")
	}
	s.Playback.ControlsVisible = false
	if got := r.Render(640, 240, s).Overlay; !bytes.Equal(got, make([]byte, len(got))) {
		t.Fatal("hidden menu retained instructions")
	}
}

func TestControlRowsFitSafeAreaAndOmitUnboundActions(t *testing.T) {
	labels := control.Labels{"open": "Long button!", "back": "Other button", "up": "Stick upward"}
	for _, size := range [][2]int{{640, 240}, {640, 288}, {320, 240}} {
		rows := controlRows(size[0], playbackHints(labels, false))
		count := 0
		for _, row := range rows {
			width := max(0, len(row)-1) * 20
			for _, hint := range row {
				width += hint.width()
				count++
			}
			if width > size[0]-48 {
				t.Fatal("instructions overflow", size, width)
			}
		}
		if count != 2 {
			t.Fatal("lost a bound action", count)
		}
		canvas := ui.NewOverlay(size[0], size[1])
		drawControls(canvas, size[1]-8-safeY(size[0], size[1]), rows)
		for y := 0; y < size[1]; y++ {
			for x := 0; x < size[0]; x++ {
				if (x < 24 || x >= size[0]-24) && canvas.Pixels[(y*size[0]+x)*4+3] != 0 {
					t.Fatal("legend escaped safe area")
				}
			}
		}
	}
	if rows := controlRows(640, playbackHints(control.Labels{}, false)); len(rows) != 0 {
		t.Fatal("unbound actions advertised", rows)
	}
}
