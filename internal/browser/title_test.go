package browser

import (
	"testing"
	"time"

	"misterfin-crt/internal/platform"
	"misterfin-crt/internal/rendering"
	"misterfin-crt/internal/videoout"
)

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
	for _, setup := range []rendering.SetupPresentation{{Kind: rendering.SetupApproval}, {Kind: rendering.SetupHidden}} {
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

}

// noticeTestRenderer captures the shared scene without depending on a display backend.
type noticeTestRenderer struct{ scene rendering.Scene }

func (r *noticeTestRenderer) Render(_, _ int, scene rendering.Scene) videoout.Frame {
	r.scene = scene
	return videoout.Frame{}
}

type noticeTestOutput struct{ videoout.Output }

func (noticeTestOutput) FrameInterval(bool) time.Duration { return time.Second / 60 }
func (noticeTestOutput) Present(videoout.Frame) error     { return nil }

func TestSettingsNoticesQueueWithoutReplacingActiveMessage(t *testing.T) {
	s := testSession(t)
	s.geometry = platform.Geometry{Width: 640, Height: 240}
	s.frameInterval = time.Second / 60
	s.output = noticeTestOutput{}
	s.renderer = &noticeTestRenderer{}
	s.startupNotices = []string{"First settings warning", "Check music configuration and assets. Music backgrounds are off."}
	s.message = rendering.MessagePresentation{Text: "Active message", Until: time.Now().Add(time.Hour)}
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
