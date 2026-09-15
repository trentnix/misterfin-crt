package browser

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"misterfin-crt/internal/platform"
	"misterfin-crt/internal/ui"
	"misterfin-crt/internal/videoout"
)

func TestRootHeadingTruncatesBeforeClock(t *testing.T) {
	for _, width := range []int{320, 640} {
		for _, height := range []int{240, 288, 480} {
			long := strings.Repeat("Wide title ", 20)
			want := string([]rune(long)[:(width-108)/16-3]) + "..."
			for _, seconds := range []float64{0, 2, 20} {
				got, expected := ui.New(width, height), ui.New(width, height)
				scene := Scene{Root: true, Title: &long, Now: time.Unix(100, 0)}
				p := screenPainter{canvas: got, width: width, height: height, safeY: safeY(width, height), scene: scene, animation: Animation{TitleSeconds: seconds}}
				p.header(scene.title(), p.safeY+4)
				p.canvas = expected
				p.scene.Root = false
				p.header(want, p.safeY+4)
				if !bytes.Equal(got.Pixels, expected.Pixels) {
					t.Fatalf("heading overflowed or scrolled at %dx%d, time %v", width, height, seconds)
				}
			}
		}
	}
	if got := (Scene{Root: true}).title(); got != "MiSTerFin CRT" {
		t.Fatal(got)
	}
	custom := "Custom"
	if got := (Scene{Title: &custom, View: View{Title: "Library"}}).title(); got != "Library" {
		t.Fatal(got)
	}
}

func TestStartupNoticeWaitsForBrowsingAndExpires(t *testing.T) {
	s := testSession(t)
	s.startupNotices = []string{"Custom background unavailable. Using normal artwork."}
	title := "Custom CRT"
	s.config.Title = &title
	s.geometry = platform.Geometry{Width: 640, Height: 240}
	s.frameInterval = time.Second / 60
	s.output = noticeTestOutput{}
	renderer := &noticeTestRenderer{}
	s.renderer = renderer
	for _, setup := range []SetupPresentation{{Kind: SetupQuickConnect}, {Kind: SetupHidden}} {
		s.setup = setup
		s.model.Current().Loading = true
		if err := s.draw(); err != nil {
			t.Fatal(err)
		}
		if len(s.startupNotices) == 0 || s.message.Text != "" {
			t.Fatal("notice consumed during sign-in or page loading")
		}
	}
	s.model.Current().Loading = false
	before := time.Now()
	if err := s.draw(); err != nil {
		t.Fatal(err)
	}
	if len(s.startupNotices) != 0 || s.message.Text == "" || renderer.scene.Title == nil || *renderer.scene.Title != "Custom CRT" {
		t.Fatal("ready screen did not receive notice and title")
	}
	until := s.message.Until
	if until.Before(before.Add(4*time.Second)) || until.After(time.Now().Add(4*time.Second)) {
		t.Fatal("wrong notice duration")
	}
	if err := s.draw(); err != nil {
		t.Fatal(err)
	}
	if s.message.Until != until {
		t.Fatal("repeated draw restarted the notice")
	}
	canvas := ui.New(640, 240)
	drawMessage(canvas, s.message, until)
	if !bytes.Equal(canvas.Pixels, make([]byte, len(canvas.Pixels))) {
		t.Fatal("expired notice remained visible")
	}
}

// noticeTestRenderer captures the shared scene without depending on a display backend.
type noticeTestRenderer struct{ scene Scene }

func (r *noticeTestRenderer) Render(_, _ int, scene Scene) videoout.Frame {
	r.scene = scene
	return videoout.Frame{}
}

type noticeTestOutput struct{ videoout.Output }

func (noticeTestOutput) FrameInterval(bool) time.Duration { return time.Second / 60 }
func (noticeTestOutput) Present(videoout.Frame) error     { return nil }

func TestEmptyTitleHidesHeadingAndKeepsClock(t *testing.T) {
	title := ""
	for _, height := range []int{240, 288, 480} {
		for _, list := range []bool{false, true} {
			scene := Scene{Root: true, ListMode: list, Title: &title, Now: time.Unix(100, 0)}
			if scene.title() != "" {
				t.Fatal("empty title restored the default")
			}
			got, expected := ui.New(640, height), ui.New(640, height)
			p := screenPainter{canvas: got, width: 640, height: height, safeY: safeY(640, height), scene: scene}
			if list {
				p.list()
			} else {
				p.carousel()
			}
			p.canvas = expected
			p.clock()
			start, end := p.safeY*640*4, (p.safeY+20)*640*4
			if !bytes.Equal(got.Pixels[start:end], expected.Pixels[start:end]) {
				t.Fatalf("hidden heading changed clock or drew title pixels: height=%d list=%v", height, list)
			}
		}
	}
}

func TestSettingsNoticesQueueWithoutReplacingActiveMessage(t *testing.T) {
	s := testSession(t)
	s.geometry = platform.Geometry{Width: 640, Height: 240}
	s.frameInterval = time.Second / 60
	s.output = noticeTestOutput{}
	s.renderer = &noticeTestRenderer{}
	s.startupNotices = []string{"First settings warning", "Check music configuration and assets. Music backgrounds are off."}
	s.message = MessagePresentation{Text: "Active message", Until: time.Now().Add(time.Hour)}
	if err := s.draw(); err != nil {
		t.Fatal(err)
	}
	if s.message.Text != "Active message" || len(s.startupNotices) != 2 {
		t.Fatal("warning replaced active message")
	}
	for remaining := 1; remaining >= 0; remaining-- {
		s.message.Until = time.Now().Add(-time.Second)
		if err := s.draw(); err != nil {
			t.Fatal(err)
		}
		if len(s.startupNotices) != remaining || s.message.Header != "Settings" {
			t.Fatal("warnings were not consumed separately")
		}
	}
}
