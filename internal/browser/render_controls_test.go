package browser

import (
	"bytes"
	"testing"
	"time"

	"misterfin-go/internal/input/control"
	"misterfin-go/internal/jellyfin"
	"misterfin-go/internal/ui"
)

func TestPreviewUsesSharedButtonBadges(t *testing.T) {
	for _, height := range []int{240, 288} {
		for _, tc := range []struct {
			name, kind string
			position   int64
			labels     control.Labels
			want       []controlHint
		}{
			{"play", "Movie", 0, control.KeyboardLabels(), []controlHint{{"Enter", "Play"}, {"Esc", "Back"}}},
			{"resume episode", "Episode", 900000000, control.KeyboardLabels(), []controlHint{{"Enter", "Resume"}, {"Tab", "Restart"}, {"Esc", "Back"}}},
			{"custom controller", "Movie", 900000000, control.Labels{"open": "Cross", "select": "Touchpad", "back": "Circle"}, []controlHint{{"Cross", "Resume"}, {"Touchpad", "Restart"}, {"Circle", "Back"}}},
			{"unbound restart", "Movie", 900000000, control.Labels{"open": "Cross", "back": "Circle"}, []controlHint{{"Cross", "Resume"}, {"Circle", "Back"}}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				item := jellyfin.Item{Name: "Preview", Type: tc.kind, RunTimeTicks: 6000000000}
				item.UserData.PlaybackPositionTicks = tc.position
				s := Scene{View: View{Detail: &item}, Controls: tc.labels, Now: time.Unix(100, 0)}
				got := renderScene(ui.New(640, height), nil, s, Animation{})
				want := ui.New(640, height)
				rows := controlRows(640, tc.want)
				bottom := height - 8 - safeY(640, height)
				drawControls(want, bottom, rows)
				start := controlsTop(bottom, rows) * 640 * 4
				if !bytes.Equal(got[start:], want.Pixels[start:]) {
					t.Fatal("preview did not render the expected shared badges")
				}
			})
		}
	}
}

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
