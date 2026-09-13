package browser

import (
	"context"
	"testing"
	"time"

	"misterfin-go/internal/jellyfin"
)

func testSession(t *testing.T) *browserSession {
	t.Helper()
	f := newControllerFixture(t)
	s := &browserSession{
		ctx: context.Background(), model: New(), controller: f.c,
		requests: requestState{cancel: func() {}},
		artwork:  artworkState{cancel: func() {}},
		media:    mediaNavigation{cancel: func() {}},
	}
	s.model.PlaybackState = f.c.state
	return s
}

func TestSessionRoutesUpWithoutRepeatingToggle(t *testing.T) {
	for _, kind := range []string{"Movie", "Audio", "Photo"} {
		t.Run(kind, func(t *testing.T) {
			s := testSession(t)
			s.model.Stack = append(s.model.Stack, View{Detail: &jellyfin.Item{Type: kind}})
			s.model.PlayingAudio = kind == "Audio"
			s.model.PlayingVideo = kind == "Movie"
			s.controller.running = kind != "Photo"
			if !s.handleKey("up") || !s.model.ControlsVisible(time.Now()) {
				t.Fatal("Up did not show controls")
			}
			if s.handleKey("up-repeat") || !s.model.ControlsVisible(time.Now()) {
				t.Fatal("held Up toggled controls")
			}
			if !s.handleKey("up") || s.model.ControlsVisible(time.Now()) {
				t.Fatal("second Up did not hide controls")
			}
		})
	}
}

func TestSessionCancelRejectsLateNeighbor(t *testing.T) {
	s := testSession(t)
	s.controller.running = false
	s.model.Stack = append(s.model.Stack, View{Detail: &jellyfin.Item{Type: "Audio"}})
	s.media.pending = true
	canceled := false
	s.media.cancel = func() { canceled = true }
	late := result{kind: neighborResult, mediaGeneration: s.media.generation, item: &jellyfin.Item{ID: "late", Type: "Audio"}}
	s.handleKey("back")
	if !canceled || s.media.pending || len(s.model.Stack) != 1 {
		t.Fatal("Back did not cancel navigation")
	}
	if s.handleResult(late) || s.model.Current().Detail != nil {
		t.Fatal("late result reopened canceled item")
	}
}

func TestSessionRejectsStaleAuthAndArtwork(t *testing.T) {
	s := testSession(t)
	s.requests.authGeneration = 2
	s.artwork.generation = 3
	s.status = "current"
	for _, r := range []result{
		{kind: authResult, request: Request{Generation: 1}, code: "stale"},
		{kind: artworkResult, imageID: 2, update: artUpdate{kind: "detail", detail: &jellyfin.Item{ID: "stale"}}},
	} {
		if s.handleResult(r) {
			t.Fatal("stale result requested redraw")
		}
	}
	if s.status != "current" || s.model.Current().Detail != nil {
		t.Fatal("stale result changed current screen")
	}
}
