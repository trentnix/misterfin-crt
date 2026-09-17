package rendering

import (
	"bytes"
	"testing"
	"time"

	"mistervision/internal/caption"
	"mistervision/internal/input/control"
	"mistervision/internal/jellyfin"
	"mistervision/internal/ui"
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
				s := Scene{Content: Content{Detail: &item, CanResume: tc.position > 0}, Controls: tc.labels, Now: time.Unix(100, 0)}
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
	expected := renderVideoOverlayOn(ui.NewOverlay(640, 240), p, s.Now, s.Controls, &caption.Renderer{})
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

func TestBrowsingUsesConfiguredBadges(t *testing.T) {
	for _, height := range []int{240, 288} {
		for _, tc := range []struct {
			name                      string
			root, list, empty, failed bool
			collection                string
			labels                    control.Labels
			want                      []controlHint
		}{
			{name: "keyboard carousel", root: true, labels: control.KeyboardLabels(), want: []controlHint{{"Left/Right", "Browse"}, {"Enter", "Select"}, {"Tab", "List"}, {"F1", "About"}, {"Esc", "Exit"}}},
			{name: "controller library", want: []controlHint{{"B", "Select"}, {"A", "Back"}}},
			{name: "root list", root: true, list: true, want: []controlHint{{"B", "Select"}, {"Select", "Carousel"}, {"A", "Exit"}}},
			{name: "music shuffle", collection: "music", labels: control.KeyboardLabels(), want: []controlHint{{"Enter", "Select"}, {"Tab", "Shuffle all"}, {"Esc", "Back"}}},
			{name: "empty library", empty: true, want: []controlHint{{"A", "Back"}}},
			{name: "custom retry", failed: true, labels: control.Labels{"open": "Cross", "back": "Circle", "retry": "Triangle"}, want: []controlHint{{"Cross", "Select"}, {"Triangle", "Retry"}, {"Circle", "Back"}}},
			{name: "unbound actions", root: true, labels: control.Labels{"next": "Right", "back": "Esc"}, want: []controlHint{{"Right", "Browse"}, {"Esc", "Exit"}}},
			{name: "wrapped custom labels", collection: "music", failed: true, labels: control.Labels{"open": "First button", "select": "Other button", "retry": "Retry button", "back": "Final button"}, want: []controlHint{{"First button", "Select"}, {"Other button", "Shuffle all"}, {"Retry button", "Retry"}, {"Final button", "Back"}}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				total := 1234
				v := Content{Title: "Library", CanShuffle: tc.collection == "music", Page: jellyfin.Page{TotalRecordCount: &total}}
				if !tc.empty {
					for i := 0; i < VisibleRows(640, height); i++ {
						v.Page.Items = append(v.Page.Items, jellyfin.Item{Name: "Selected item", Type: "MusicArtist"})
					}
				}
				if tc.failed {
					v.Error = "Could not load library"
				}
				s := Scene{Content: v, Root: tc.root, ListMode: tc.list, Controls: tc.labels, Now: time.Unix(100, 0)}
				got := renderScene(ui.New(640, height), nil, s, Animation{})
				want := ui.New(640, height)
				rows := controlRows(640, tc.want)
				bottom := height - 8 - safeY(640, height)
				drawControls(want, bottom, rows)
				start := controlsTop(bottom, rows) * 640 * 4
				if !bytes.Equal(got[start:], want.Pixels[start:]) {
					t.Fatal("browsing badges differ or overlap list content, count, or error")
				}
			})
		}
	}
}

func TestTrackMenuAndSubtitlesUseSharedOverlay(t *testing.T) {
	now := time.Unix(100, 0)
	p := PlaybackPresentation{Tracks: &TrackMenu{Rows: []TrackRow{{Index: -1, Label: "Off", Active: true}, {Index: 1, Label: "English"}}}}
	for _, height := range []int{240, 288} {
		s := Scene{Video: true, Playback: p, Controls: control.KeyboardLabels(), Now: now}
		renderer := NewRenderer()
		frame := renderer.Render(640, height, s)
		if !bytes.Equal(frame.Overlay, renderVideoOverlayOn(ui.NewOverlay(640, height), p, now, s.Controls, &caption.Renderer{})) {
			t.Fatal("track picker bypassed shared renderer")
		}
		if bytes.Equal(frame.Overlay, make([]byte, len(frame.Overlay))) {
			t.Fatal("track picker is invisible")
		}
		s.Playback.Tracks = nil
		s.Playback.Subtitle = "A shared subtitle"
		s.Playback.ControlsVisible = false
		subtitle := append([]byte(nil), renderer.Render(640, height, s).Overlay...)
		if bytes.Equal(subtitle, make([]byte, len(subtitle))) {
			t.Fatal("hidden controls hid subtitles")
		}
		s.Playback.Subtitle = ""
		if !bytes.Equal(renderer.Render(640, height, s).Overlay, make([]byte, len(subtitle))) {
			t.Fatal("expired cue left stale pixels")
		}
	}
}

func TestViewTabsSharePanelBoundsAndOpacity(t *testing.T) {
	now := time.Unix(100, 0)
	p := PlaybackPresentation{Tracks: &TrackMenu{Rows: []TrackRow{{Index: -1, Label: "Off", Active: true}, {Index: 1, Label: "English"}}}}
	for _, height := range []int{240, 288} {
		var want []byte
		for tab := 0; tab < 3; tab++ {
			p.Tracks.Tab = tab
			frame := renderVideoOverlayOn(ui.NewOverlay(640, height), p, now, control.KeyboardLabels(), &caption.Renderer{})
			// At x=13 only the panel background is drawn, so this column captures
			// both its vertical extent and opacity independently of the tab's text.
			column := make([]byte, height)
			for y := 0; y < height; y++ {
				column[y] = frame[(y*640+13)*4+3]
			}
			if tab == 0 {
				want = column
			} else if !bytes.Equal(column, want) {
				t.Fatal("View tabs use different panel bounds or opacity")
			}
			if frame[((height/4)*640+13)*4+3] != 225 {
				t.Fatal("panel no longer covers the full View area")
			}
		}
	}
}

func TestUnicodeSubtitlesUseFallbackOnBothOutputSizes(t *testing.T) {
	for _, height := range []int{240, 480} {
		s := Scene{Video: true, Playback: PlaybackPresentation{Subtitle: "††† ♪ Don’t go — please…"}}
		renderer := NewRenderer()
		frame := append([]byte(nil), renderer.Render(640, height, s).Overlay...)
		s.Playback.Subtitle = "??? ? Don?t go ? please?"
		replaced := renderer.Render(640, height, s).Overlay
		if bytes.Equal(frame, replaced) {
			t.Fatalf("Unicode subtitles replaced at height %d", height)
		}
	}
}

func TestCaptionSurvivesControlsAndClears(t *testing.T) {
	for _, height := range []int{240, 480} {
		r := NewRenderer()
		s := Scene{Video: true, Playback: PlaybackPresentation{Subtitle: "日本語の字幕 مرحبا"}}
		initial := bytes.Clone(r.Render(640, height, s).Overlay)
		s.Playback.ControlsVisible = true
		controls := bytes.Clone(r.Render(640, height, s).Overlay)
		if bytes.Equal(initial, controls) {
			t.Fatal("controls did not move caption")
		}
		s.Playback.ControlsVisible = false
		if !bytes.Equal(initial, r.Render(640, height, s).Overlay) {
			t.Fatal("hiding controls altered caption")
		}
		s.Playback.Subtitle = ""
		for _, value := range r.Render(640, height, s).Overlay {
			if value != 0 {
				t.Fatal("caption left stale pixels after clearing")
			}
		}
	}
}

func BenchmarkVideoCaption(b *testing.B) {
	r := NewRenderer()
	s := Scene{Video: true, Playback: PlaybackPresentation{Subtitle: "The quick brown fox jumps over the lazy dog."}}
	r.Render(640, 240, s)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Render(640, 240, s)
	}
}
