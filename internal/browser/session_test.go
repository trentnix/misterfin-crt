package browser

import (
	"context"
	"testing"
	"time"

	"mistervision/internal/input/control"
	"mistervision/internal/media"
	"mistervision/internal/playback"
	"mistervision/internal/remote"
	"mistervision/internal/rendering"
	"mistervision/internal/videoout"
)

func testSession(t *testing.T) *browserSession {
	t.Helper()
	f := newControllerFixture(t)
	s := &browserSession{
		ctx: context.Background(), model: New(), controller: f.c,
		events: make(chan workerResult, 16), output: sessionTestOutput{},
		connection: newConnectionManager(Config{}, 640, 240),
		requests:   requestState{cancel: func() {}},
		selection:  selectionState{cancel: func() {}},
		media:      mediaNavigation{cancel: func() {}},
	}
	return s
}

func TestSessionRoutesUpWithoutRepeatingToggle(t *testing.T) {
	for _, kind := range []string{"Movie", "Audio", "Photo"} {
		t.Run(kind, func(t *testing.T) {
			s := testSession(t)
			s.model.Stack = append(s.model.Stack, View{Detail: &media.Item{Type: kind}})
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
			if !s.handleKey(control.Up) || !controlsVisible() {
				t.Fatal("Up did not show controls")
			}
			if s.handleKey("up-repeat") || !controlsVisible() {
				t.Fatal("held Up toggled controls")
			}
			if !s.handleKey(control.Up) || controlsVisible() {
				t.Fatal("second Up did not hide controls")
			}
		})
	}
}

func TestSessionCancelRejectsLateNeighbor(t *testing.T) {
	s := testSession(t)
	s.controller.running = false
	s.model.Stack = append(s.model.Stack, View{Detail: &media.Item{Type: "Audio"}})
	s.media.pending = true
	canceled := false
	s.media.cancel = func() { canceled = true }
	late := neighborResult{generation: s.media.generation, item: &media.Item{ID: "late", Type: "Audio"}}
	s.handleKey(control.Back)
	if !canceled || s.media.pending || len(s.model.Stack) != 1 {
		t.Fatal("Back did not cancel navigation")
	}
	if s.handleResult(late) || s.model.Current().Detail != nil {
		t.Fatal("late result reopened canceled item")
	}
}

func TestSessionRejectsStaleAuthAndSelection(t *testing.T) {
	s := testSession(t)
	s.connection.generation = 2
	s.selection.generation = 3
	s.setup = rendering.SetupPresentation{Kind: rendering.SetupApproval, Code: "current"}
	for _, r := range []workerResult{
		authCodeResult{generation: 1, presentation: rendering.SetupPresentation{Code: "stale"}},
		selectionResult{generation: 2, update: selectionUpdate{kind: selectionDetails, detail: &media.Item{ID: "stale"}}},
		selectionResult{generation: 2, update: selectionUpdate{kind: selectionCount, count: new(int)}},
		selectionResult{generation: 2, update: selectionUpdate{kind: selectionArtwork, art: artUpdate{kind: "cover", total: 1}}},
	} {
		if s.handleResult(r) {
			t.Fatal("stale result requested redraw")
		}
	}
	if s.setup.Code != "current" || s.model.Current().Detail != nil || s.selection.current.count != nil || s.selection.current.artwork.Covers != nil {
		t.Fatal("stale result changed current screen")
	}
}

// These handler tests only clear output. Rendering has separate contract tests.
type sessionTestOutput struct{ videoout.Output }

func (sessionTestOutput) Clear() {}

func setupMusicSession(t *testing.T) *browserSession {
	t.Helper()
	s := testSession(t)
	tracks := []media.Item{
		{ID: "first", Name: "First", Type: "Audio"},
		{ID: "second", Name: "Second", Type: "Audio"},
	}
	s.model.Current().Location.Kind = "items"
	s.model.Current().Page.Items = tracks
	s.model.Key(control.Open)
	s.startPlayback(nil, false)
	return s
}

func receiveNeighbor(t *testing.T, s *browserSession) neighborResult {
	t.Helper()
	select {
	case r := <-s.events:
		neighbor, ok := r.(neighborResult)
		if !ok {
			t.Fatalf("unexpected result: %+v", r)
		}
		return neighbor
	case <-time.After(time.Second):
		t.Fatal("neighbor request did not complete")
		return neighborResult{}
	}
}

func TestMusicQueueSurvivesDecoderCompletion(t *testing.T) {
	s := setupMusicSession(t)
	now := time.Now()
	s.controller.Key(control.ToggleControls, now)
	if !s.handlePlayback(PlaybackEvent{Kind: PlaybackEnded, ID: s.controller.active.id}) {
		t.Fatal("track completion did not request a redraw")
	}
	snapshot := s.controller.Snapshot(now)
	scene := sceneFromModel(s.model, snapshot, rendering.SetupPresentation{}, selectionData{}, "", now)
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
	s.handleKey(control.TrackNext)
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
		for _, key := range []control.Action{"up", "down", "previous", "next"} {
			t.Run(kind+"/"+string(key), func(t *testing.T) {
				s := testSession(t)
				s.model.Stack = append(s.model.Stack, View{Detail: &media.Item{Type: kind}})
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
	s.model.Stack = append(s.model.Stack, View{Detail: &media.Item{Type: "Audio"}})
	s.model.StartMusicQueue()
	s.controller.item.Type = "Audio"
	s.controller.running = true
	s.controller.state.ProgressSeen = true
	for _, tc := range []struct {
		key     control.Action
		seconds int
	}{{"seek-backward", -10}, {"seek-forward", 10}, {"seek-forward-repeat", 10}} {
		s.handleKey(tc.key)
		select {
		case c := <-s.controller.active.controls:
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

func TestPlaylistPauseSurvivesLocalTransition(t *testing.T) {
	for _, phase := range []string{"resolving", "stopping", "between_tracks"} {
		for _, input := range []struct {
			name   string
			apply  func(*browserSession)
			paused bool
		}{
			{"remote_pause", func(s *browserSession) { s.handleRemote(remote.Command{Kind: remote.Pause}) }, true},
			{"remote_pause_toggle", func(s *browserSession) {
				s.handleRemote(remote.Command{Kind: remote.Pause})
				s.handleRemote(remote.Command{Kind: remote.TogglePause})
			}, false},
			{"remote_resume_toggle", func(s *browserSession) {
				s.handleRemote(remote.Command{Kind: remote.Resume})
				s.handleRemote(remote.Command{Kind: remote.TogglePause})
			}, true},
			{"local_toggle", func(s *browserSession) { s.handleKey(control.Open) }, true},
			{"local_two_toggles", func(s *browserSession) { s.handleKey(control.Open); s.handleKey(control.Open) }, false},
		} {
			t.Run(phase+"/"+input.name, func(t *testing.T) {
				s := setupMusicSession(t)
				s.model.Stack[len(s.model.Stack)-2].Location.Kind = "playlist"
				old := s.controller.active.id
				s.handlePlayback(PlaybackEvent{Kind: PlaybackPosition, ID: old, Ticks: 10000000})
				if phase == "between_tracks" {
					s.handlePlayback(PlaybackEvent{Kind: PlaybackEnded, ID: old})
				} else {
					s.handleRemote(remote.Command{Kind: remote.Next})
					if phase == "stopping" {
						s.handleNeighbor(receiveNeighbor(t, s))
					}
				}
				input.apply(s)
				if phase != "stopping" {
					s.handleNeighbor(receiveNeighbor(t, s))
				}
				if phase != "between_tracks" {
					s.handlePlayback(PlaybackEvent{Kind: PlaybackEnded, ID: old})
				}
				s.handlePlayback(PlaybackEvent{Kind: PlaybackPosition, ID: s.controller.active.id, Ticks: 1})
				if s.controller.item.ID != "second" || s.controller.wantsPause() != input.paused {
					t.Fatalf("next item=%q paused=%v, want paused=%v", s.controller.item.ID, s.controller.wantsPause(), input.paused)
				}
				if input.paused {
					expectCommand(t, s.controller.active.controls, playback.SetPaused)
				}
				if len(s.controller.active.controls) != 0 {
					t.Fatal("unexpected command reached replacement")
				}
			})
		}
	}
}

func TestNextAndPreviousStartPlayingAfterPausedItem(t *testing.T) {
	for _, queue := range []string{"library", "playlist", "remote", "shuffle"} {
		for _, command := range []remote.Kind{remote.Next, remote.Previous} {
			t.Run(queue+"/"+string(command), func(t *testing.T) {
				s := testSession(t)
				kind := "Audio"
				if queue == "playlist" || queue == "remote" {
					kind = "Movie"
				}
				items := []media.Item{{ID: "first", Type: kind}, {ID: "middle", Type: kind}, {ID: "last", Type: kind}}
				v := s.model.Current()
				v.Location.Kind = "items"
				if queue == "playlist" {
					v.Location.Kind = "playlist"
				}
				v.Page.Items = items
				v.Selected = 1
				s.model.Key(control.Open)
				s.startPlayback(nil, false)
				if queue == "remote" {
					s.adoptLocalQueue()
					s.playbackQueue.replace(items, 1)
				}
				if queue == "shuffle" {
					s.shuffle = shuffleQueue{library: "music", items: items, position: 1}
				}
				old := s.controller.active.id
				s.handlePlayback(PlaybackEvent{Kind: PlaybackPosition, ID: old, Ticks: 10000000})
				setControllerPaused(t, s.controller, true, time.Now())
				s.handleRemote(remote.Command{Kind: command})
				if queue == "library" || queue == "playlist" {
					s.handleNeighbor(receiveNeighbor(t, s))
				}
				s.handlePlayback(PlaybackEvent{Kind: PlaybackEnded, ID: old})
				s.handlePlayback(PlaybackEvent{Kind: PlaybackPosition, ID: s.controller.active.id, Ticks: 1})
				want := "last"
				if command == remote.Previous {
					want = "first"
				}
				if s.controller.item.ID != want || s.controller.wantsPause() || len(s.controller.active.controls) != 0 {
					t.Fatalf("next item=%q paused=%v: navigation inherited the previous pause", s.controller.item.ID, s.controller.wantsPause())
				}
			})
		}
	}
}

func TestCanceledPlaylistTransitionDiscardsPause(t *testing.T) {
	s := setupMusicSession(t)
	s.model.Stack[len(s.model.Stack)-2].Location.Kind = "playlist"
	old := s.controller.active.id
	s.handleRemote(remote.Command{Kind: remote.Next})
	result := receiveNeighbor(t, s)
	s.handleRemote(remote.Command{Kind: remote.Pause})
	s.handleRemote(remote.Command{Kind: remote.Stop})
	if s.handleNeighbor(result) {
		t.Fatal("stopped transition accepted late item")
	}
	s.handlePlayback(PlaybackEvent{Kind: PlaybackEnded, ID: old})
	if s.controller.running || s.localPlaybackPending() {
		t.Fatal("canceled playback remained active")
	}
	s.model.Key(control.Open)
	s.startPlayback(nil, false)
	s.handlePlayback(PlaybackEvent{Kind: PlaybackPosition, ID: s.controller.active.id, Ticks: 1})
	if s.wantsPause() || len(s.controller.active.controls) != 0 {
		t.Fatal("new playback inherited canceled pause")
	}
}
