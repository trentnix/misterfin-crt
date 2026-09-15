package browser

import (
	"context"
	"sync"
	"testing"
	"time"

	"misterfin-crt/internal/input/control"
	"misterfin-crt/internal/jellyfin"
	"misterfin-crt/internal/playback"
	"misterfin-crt/internal/sound"
)

func TestBrowsingSoundsFollowChangesNotRawKeys(t *testing.T) {
	s := testSession(t)
	s.controller.running = false
	recorder := &testFeedback{}
	s.feedback = recorder
	v := s.model.Current()
	v.Page = jellyfin.Page{Items: []jellyfin.Item{{ID: "one", Name: "One"}, {ID: "two", Name: "Two"}}}
	s.handleKey(control.Previous) // At the first item: no change.
	s.handleKey(control.Up)       // Carousel ignores vertical navigation.
	if len(recorder.cues) != 0 {
		t.Fatal("unchanged selection made a sound")
	}
	s.handleKey(control.Next)
	s.handleKey("next-repeat") // At the last item: no change.
	if len(recorder.cues) != 1 || recorder.cues[0] != sound.Navigate {
		t.Fatalf("cues: %v", recorder.cues)
	}
	s.handleKey(control.Select) // Switch to list mode.
	s.handleKey("select-repeat")
	s.handleKey(control.Back) // Exit confirmation.
	if len(recorder.cues) != 3 || recorder.cues[1] != sound.Confirm || recorder.cues[2] != sound.Confirm {
		t.Fatalf("cues: %v", recorder.cues)
	}
}

func TestPlaybackControlsRemainSilent(t *testing.T) {
	s := testSession(t)
	recorder := &testFeedback{}
	s.feedback = recorder
	s.controller.running = true
	s.controller.item.Type = "Movie"
	s.controller.state.PlayingVideo = true
	s.handleKey(control.Up)
	s.handleKey(control.Up)
	s.handleKey(control.Open)
	if len(recorder.cues) != 0 {
		t.Fatalf("playback cues: %v", recorder.cues)
	}
}

func TestFailedDecoderLaunchReleasesSoundSuspension(t *testing.T) {
	recorder := &testFeedback{}
	driver := playbackDriver{ctx: context.Background(), feedback: recorder, output: sessionTestOutput{},
		config: playback.Config{VideoDecoder: playback.DecoderConfig{Kind: playback.DecoderFFplay, Player: "/missing/misterfin-test-player"}},
		events: make(chan PlaybackEvent, 16)}
	process := driver.launch(&jellyfin.Client{}, jellyfin.Item{Type: "Movie"}, nil, nil, false, nil, playback.TrackOptions{})
	select {
	case <-process.done:
	case <-time.After(time.Second):
		t.Fatal("failed launch did not complete")
	}
	if recorder.suspended != 1 || recorder.resumed != 1 {
		t.Fatalf("suspend=%d resume=%d", recorder.suspended, recorder.resumed)
	}
}

// testFeedback captures semantic cues and enforces idempotent playback release.
type testFeedback struct {
	cues               []sound.Cue
	suspended, resumed int
}

func (f *testFeedback) Play(c sound.Cue) { f.cues = append(f.cues, c) }
func (f *testFeedback) Suspend() func()  { f.suspended++; return sync.OnceFunc(func() { f.resumed++ }) }
