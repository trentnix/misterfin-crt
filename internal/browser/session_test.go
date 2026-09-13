package browser

import (
	"context"
	"testing"
	"time"

	"misterfin-go/internal/jellyfin"
	"misterfin-go/internal/videoout"
)

func testSession(t *testing.T) *browserSession {
	t.Helper()
	f := newControllerFixture(t)
	s := &browserSession{
		ctx: context.Background(), model: New(), controller: f.c,
		events: make(chan result, 16), output: sessionTestOutput{},
		requests:  requestState{cancel: func() {}},
		selection: selectionState{cancel: func() {}},
		media:     mediaNavigation{cancel: func() {}},
	}
	return s
}

func TestSessionRoutesUpWithoutRepeatingToggle(t *testing.T) {
	for _, kind := range []string{"Movie", "Audio", "Photo"} {
		t.Run(kind, func(t *testing.T) {
			s := testSession(t)
			s.model.Stack = append(s.model.Stack, View{Detail: &jellyfin.Item{Type: kind}})
			s.controller.item.Type = kind
			s.controller.state.PlayingVideo = kind == "Movie"
			if kind == "Audio" {
				s.model.StartMusicQueue()
			}
			controlsVisible := func() bool {
				if kind == "Photo" {
					return s.model.PhotoControlsVisible(time.Now())
				}
				return s.controller.Snapshot(time.Now()).ControlsVisible
			}
			s.controller.running = kind != "Photo"
			if !s.handleKey("up") || !controlsVisible() {
				t.Fatal("Up did not show controls")
			}
			if s.handleKey("up-repeat") || !controlsVisible() {
				t.Fatal("held Up toggled controls")
			}
			if !s.handleKey("up") || controlsVisible() {
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

func TestSessionRejectsStaleAuthAndSelection(t *testing.T) {
	s := testSession(t)
	s.requests.authGeneration = 2
	s.selection.generation = 3
	s.status = "current"
	for _, r := range []result{
		{kind: authResult, request: Request{Generation: 1}, code: "stale"},
		{kind: selectionResult, selectionGeneration: 2, update: selectionUpdate{kind: selectionDetails, detail: &jellyfin.Item{ID: "stale"}}},
		{kind: selectionResult, selectionGeneration: 2, update: selectionUpdate{kind: selectionCount, count: new(int)}},
		{kind: selectionResult, selectionGeneration: 2, update: selectionUpdate{kind: selectionArtwork, art: artUpdate{kind: "cover", total: 1}}},
	} {
		if s.handleResult(r) {
			t.Fatal("stale result requested redraw")
		}
	}
	if s.status != "current" || s.model.Current().Detail != nil || s.selection.current.count != nil || s.selection.current.artwork.Covers != nil {
		t.Fatal("stale result changed current screen")
	}
}

// These handler tests only clear output. Rendering has separate contract tests.
type sessionTestOutput struct{ videoout.Output }

func (sessionTestOutput) Clear() {}

func setupMusicSession(t *testing.T) *browserSession {
	t.Helper()
	s := testSession(t)
	tracks := []jellyfin.Item{
		{ID: "first", Name: "First", Type: "Audio"},
		{ID: "second", Name: "Second", Type: "Audio"},
	}
	s.model.Current().Location.Kind = "items"
	s.model.Current().Page.Items = tracks
	s.model.Key("open")
	s.startPlayback(nil, false)
	return s
}

func receiveNeighbor(t *testing.T, s *browserSession) result {
	t.Helper()
	select {
	case r := <-s.events:
		if r.kind != neighborResult {
			t.Fatalf("unexpected result: %+v", r)
		}
		return r
	case <-time.After(time.Second):
		t.Fatal("neighbor request did not complete")
		return result{}
	}
}

func TestMusicQueueSurvivesDecoderCompletion(t *testing.T) {
	s := setupMusicSession(t)
	now := time.Now()
	s.controller.Key("controls", now)
	if !s.handlePlayback(PlaybackEvent{Kind: PlaybackEnded, ID: s.controller.active.id}) {
		t.Fatal("track completion did not request a redraw")
	}
	snapshot := s.controller.Snapshot(now)
	scene := sceneFromModel(s.model, snapshot, "", selectionData{}, "", now)
	if snapshot.Active || !scene.Audio || scene.Video || !scene.Playback.ControlsVisible || !s.media.pending {
		t.Fatalf("between-track screen changed: %+v", scene)
	}
	if s.model.Current().Detail.ID != "first" {
		t.Fatal("selection moved before neighbor arrived")
	}
	s.handleNeighbor(receiveNeighbor(t, s))
	if !s.controller.running || !s.model.MusicQueueActive() || s.model.Current().Title != "Second" {
		t.Fatal("next track did not start on the music screen")
	}
	if snapshot.Active || !snapshot.ControlsVisible {
		t.Fatal("starting next track mutated old snapshot")
	}
	s.handlePlayback(PlaybackEvent{Kind: PlaybackEnded, ID: s.controller.active.id})
	s.handleNeighbor(receiveNeighbor(t, s))
	if s.model.MusicQueueActive() || s.controller.running || len(s.model.Stack) != 1 || s.model.Current().Selected != 1 {
		t.Fatal("queue completion did not restore the last track in the list")
	}
}

func TestTrackSelectionWaitsForDecoderExit(t *testing.T) {
	s := setupMusicSession(t)
	s.handleKey("track-next-repeat")
	if s.media.pending {
		t.Fatal("held shoulder changed tracks")
	}
	s.handleKey("track-next")
	s.handleNeighbor(receiveNeighbor(t, s))
	if s.media.queued == nil || s.model.Current().Detail.ID != "first" {
		t.Fatal("neighbor selection did not wait for the active decoder")
	}
	s.handlePlayback(PlaybackEvent{Kind: PlaybackEnded, ID: s.controller.active.id})
	parent, _ := s.model.Parent()
	if s.media.queued != nil || s.model.Current().Detail.ID != "second" || parent.Selected != 1 || !s.controller.running {
		t.Fatal("decoder exit did not commit the queued selection and start playback")
	}
	s.handleMusicKey("back")
	s.handlePlayback(PlaybackEvent{Kind: PlaybackEnded, ID: s.controller.active.id})
	if s.model.MusicQueueActive() || len(s.model.Stack) != 1 || s.model.Current().Selected != 1 {
		t.Fatal("stop did not restore the selected track in the list")
	}
}

func TestPlaybackDirectionsOnlyToggleMenu(t *testing.T) {
	for _, kind := range []string{"Movie", "Episode", "TvChannel", "Audio"} {
		for _, key := range []string{"up", "down", "previous", "next"} {
			t.Run(kind+"/"+key, func(t *testing.T) {
				s := testSession(t)
				s.model.Stack = append(s.model.Stack, View{Detail: &jellyfin.Item{Type: kind}})
				s.controller.item.Type = kind
				s.controller.running = true
				s.controller.state.ProgressSeen = true
				s.controller.state.PlayingVideo = kind != "Audio"
				if kind == "Audio" {
					s.model.StartMusicQueue()
				}
				s.handleKey(key)
				if !s.controller.Snapshot(time.Now()).ControlsVisible {
					t.Fatal("direction did not show menu")
				}
				for i := 0; i < 10; i++ {
					s.handleKey(key + "-repeat")
				}
				if !s.controller.Snapshot(time.Now()).ControlsVisible {
					t.Fatal("held direction toggled menu")
				}
				s.handleKey(key)
				if s.controller.Snapshot(time.Now()).ControlsVisible || s.controller.state.SeekTarget != nil || s.media.pending {
					t.Fatal("direction changed playback or failed to hide menu")
				}
			})
		}
	}
}

func TestMusicSeekingDoesNotChangeTrackOrShowControls(t *testing.T) {
	s := testSession(t)
	s.model.Stack = append(s.model.Stack, View{Detail: &jellyfin.Item{Type: "Audio"}})
	s.model.StartMusicQueue()
	s.controller.item.Type = "Audio"
	s.controller.running = true
	s.controller.state.ProgressSeen = true
	for _, tc := range []struct {
		key     string
		seconds int
	}{{"seek-backward", -10}, {"seek-forward", 10}, {"seek-forward-repeat", 10}} {
		s.handleKey(tc.key)
		select {
		case c := <-s.controller.controls:
			if c.Kind != "seek" || c.Seconds != tc.seconds {
				t.Fatal(c)
			}
		default:
			t.Fatal("seek command missing")
		}
	}
	if s.media.pending || s.controller.Snapshot(time.Now()).ControlsVisible {
		t.Fatal("seeking changed track or showed menu")
	}
}
