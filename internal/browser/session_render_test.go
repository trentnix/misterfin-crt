package browser

import (
	"errors"
	"testing"
	"time"

	"mistervision/internal/platform"
	"mistervision/internal/playback"
	"mistervision/internal/rendering"
	"mistervision/internal/videoout"
)

type pausedOverlayRenderer struct{}

func (pausedOverlayRenderer) Render(int, int, rendering.Scene) videoout.Frame {
	return videoout.Frame{Video: true, Overlay: []byte{1, 2, 3, 255}}
}

type pausedOverlayOutput struct {
	noticeTestOutput
	err error
}

func (o *pausedOverlayOutput) Present(videoout.Frame) error { return o.err }

func pausedOverlaySession(t *testing.T) *browserSession {
	t.Helper()
	s := testSession(t)
	s.geometry = platform.Geometry{Width: 640, Height: 240}
	s.frameInterval = time.Second / 60
	s.output = noticeTestOutput{}
	s.renderer = pausedOverlayRenderer{}
	s.controller.state.Paused = true
	return s
}

func TestPausedOverlayRetriesBusyDecoder(t *testing.T) {
	s := pausedOverlaySession(t)
	for range cap(s.controller.active.controls) {
		s.controller.active.controls <- playback.Control{Kind: playback.Report}
	}
	if err := s.draw(); err != nil {
		t.Fatal(err)
	}
	for len(s.controller.active.controls) > 0 {
		<-s.controller.active.controls
	}
	if err := s.draw(); err != nil {
		t.Fatal(err)
	}
	expectCommand(t, s.controller.active.controls, playback.Refresh)
	if err := s.draw(); err != nil {
		t.Fatal(err)
	}
	if len(s.controller.active.controls) != 0 {
		t.Fatal("unchanged overlay sent another refresh")
	}
}

func TestPausedOverlayRetriesFailedPresentation(t *testing.T) {
	s := pausedOverlaySession(t)
	output := &pausedOverlayOutput{err: errors.New("display unavailable")}
	s.output = output
	if err := s.draw(); !errors.Is(err, output.err) {
		t.Fatal("missing display error", err)
	}
	if len(s.controller.active.controls) != 0 {
		t.Fatal("failed presentation requested a refresh")
	}
	output.err = nil
	if err := s.draw(); err != nil {
		t.Fatal(err)
	}
	expectCommand(t, s.controller.active.controls, playback.Refresh)
}

func TestPausedOverlayRefreshesAfterResuming(t *testing.T) {
	s := pausedOverlaySession(t)
	if err := s.draw(); err != nil {
		t.Fatal(err)
	}
	expectCommand(t, s.controller.active.controls, playback.Refresh)
	s.controller.state.Paused = false
	if err := s.draw(); err != nil {
		t.Fatal(err)
	}
	if len(s.controller.active.controls) != 0 {
		t.Fatal("playing video requested a refresh")
	}
	s.controller.state.Paused = true
	if err := s.draw(); err != nil {
		t.Fatal(err)
	}
	expectCommand(t, s.controller.active.controls, playback.Refresh)
}
