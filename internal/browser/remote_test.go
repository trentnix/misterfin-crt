package browser

import (
	"bytes"
	"context"
	"testing"
	"time"

	"mistervision/internal/input/control"
	"mistervision/internal/media"
	"mistervision/internal/playback"
	"mistervision/internal/remote"
	"mistervision/internal/rendering"
)

func TestRemoteCommandsIgnoreMenusAndPreserveLabels(t *testing.T) {
	s := testSession(t)
	s.about.Visible = true
	s.controller.picker.visible = true
	s.handleRemote(remote.Command{Kind: remote.Pause})
	expectCommand(t, s.controller.active.controls, "set-pause")
	s.handleRemote(remote.Command{Kind: remote.Pause})
	expectCommand(t, s.controller.active.controls, "set-pause")
	s.handleRemote(remote.Command{Kind: remote.Resume})
	expectCommand(t, s.controller.active.controls, "resume")
	if s.controller.wantsPause() {
		t.Fatal("remote pause scheduled an extra toggle")
	}
	s.handleRemote(remote.Command{Kind: remote.Stop})
	if !s.controller.stoppedByUser || s.model.Quit {
		t.Fatal("Stop did not target playback")
	}
}

func TestRemoteQueueSwitchesOnceAndAdvancesOnCompletion(t *testing.T) {
	s := testSession(t)
	items := []media.Item{{ID: "a", Type: "Audio", Name: "A"}, {ID: "b", Type: "Audio", Name: "B"}, {ID: "c", Type: "Audio", Name: "C"}}
	s.applyRemoteItems(remote.Command{PlayMode: "now", StartIndex: 1}, items)
	if !s.playbackQueue.switching {
		t.Fatal("replacement skipped cleanup")
	}
	s.handlePlayback(PlaybackEvent{Kind: PlaybackEnded, ID: 1})
	if s.controller.item.ID != "b" {
		t.Fatal("StartIndex ignored")
	}
	s.handleRemote(remote.Command{Kind: remote.Previous})
	s.handlePlayback(PlaybackEvent{Kind: PlaybackEnded, ID: 2})
	if s.controller.item.ID != "a" {
		t.Fatal("previous required multiple presses")
	}
	s.handleRemote(remote.Command{Kind: remote.Repeat, Repeat: remote.RepeatOne})
	s.handlePlayback(PlaybackEvent{Kind: PlaybackEnded, ID: 3})
	if s.controller.item.ID != "a" || !s.controller.running {
		t.Fatal("repeat one failed")
	}
	s.handleRemote(remote.Command{Kind: remote.Next})
	s.handlePlayback(PlaybackEvent{Kind: PlaybackEnded, ID: 4})
	if s.controller.item.ID != "b" {
		t.Fatal("explicit Next did not override repeat one")
	}
	s.handleRemote(remote.Command{Kind: remote.Stop})
	s.handlePlayback(PlaybackEvent{Kind: PlaybackEnded, ID: 5})
	if s.playbackQueue.active || s.controller.running || len(s.model.Stack) != 1 {
		t.Fatal("Stop did not restore browsing")
	}
}

func TestRapidNextAccumulatesAndStopCancelsReplacement(t *testing.T) {
	s := testSession(t)
	s.applyRemoteItems(remote.Command{PlayMode: "now"}, []media.Item{{ID: "a", Type: "Audio"}, {ID: "b", Type: "Audio"}, {ID: "c", Type: "Audio"}})
	s.handlePlayback(PlaybackEvent{Kind: PlaybackEnded, ID: 1})
	s.handleRemote(remote.Command{Kind: remote.Next})
	s.handleRemote(remote.Command{Kind: remote.Next})
	s.handlePlayback(PlaybackEvent{Kind: PlaybackEnded, ID: 2})
	if s.controller.item.ID != "c" {
		t.Fatal("rapid next lost a command")
	}
	s.handleRemote(remote.Command{Kind: remote.Previous})
	s.handleRemote(remote.Command{Kind: remote.Stop})
	s.handlePlayback(PlaybackEvent{Kind: PlaybackEnded, ID: 3})
	if s.controller.running {
		t.Fatal("stopped replacement reopened playback")
	}
}

func TestRemoteSeekAndPauseDuringHandoff(t *testing.T) {
	f := newControllerFixture(t)
	target := int64(90 * 10000000)
	f.c.SeekTo(target, f.now)
	f.c.Tick(f.now.Add(time.Second))
	f.c.SetPaused(true)
	if !f.c.wantsPause() {
		t.Fatal("lost remote pause intent")
	}
	f.c.SetPaused(false)
	if f.c.wantsPause() {
		t.Fatal("lost remote resume intent")
	}
	f.c.SeekTo(target+10000000, f.now.Add(time.Second))
	if f.c.seekPhase != seekRetargeting {
		t.Fatal("did not retarget")
	}
	live := newControllerFixture(t)
	live.c.item.Type = "TvChannel"
	live.c.SeekTo(target, live.now)
	if live.c.state.SeekTarget != nil {
		t.Fatal("Live TV accepted seek")
	}
}

func TestRemoteMessageUsesSharedOverlay(t *testing.T) {
	now := time.Now()
	scene := rendering.Scene{Now: now, Video: true, Playback: rendering.PlaybackPresentation{Active: true}, Message: rendering.MessagePresentation{Text: "Remote message", Until: now.Add(time.Second)}}
	renderer := rendering.NewRenderer()
	frame := renderer.Render(640, 240, scene)
	visible := append([]byte(nil), frame.Overlay...)
	scene.Now = now.Add(2 * time.Second)
	frame = renderer.Render(640, 240, scene)
	if bytes.Equal(visible, frame.Overlay) {
		t.Fatal("banner did not expire")
	}
}

func TestCanceledRemoteResultDoesNotStartPlayback(t *testing.T) {
	s := testSession(t)
	s.remoteRequests.cancelAll()
	if (remoteItemsResult{generation: 0, items: []media.Item{{ID: "stale", Type: "Audio"}}}).apply(s) {
		t.Fatal("stale result accepted")
	}
	s.remote.generation = 2
	if (remoteCommandResult{generation: 1, command: remote.Command{Kind: remote.Stop}}).apply(s) {
		t.Fatal("old account command accepted")
	}
}

func TestLocalBackClosesPickerBeforeRemoteQueue(t *testing.T) {
	s := testSession(t)
	s.playbackQueue.active = true
	s.controller.picker.visible = true
	s.handleKey(control.Back)
	if s.controller.stoppedByUser || s.controller.picker.visible {
		t.Fatal("Back stopped playback instead of closing View")
	}
}

func TestLocalLiveQueueReturnsToChannelList(t *testing.T) {
	s := testSession(t)
	s.controller.item.Type = "TvChannel"
	s.model.Stack = append(s.model.Stack, View{Title: "Channels"}, View{Detail: &s.controller.item})
	s.adoptLocalQueue()
	s.endQueue()
	if len(s.model.Stack) != 2 || s.model.Current().Title != "Channels" {
		t.Fatal("live queue retained channel detail")
	}
}

func TestQueueReorderKeepsCurrentDecoder(t *testing.T) {
	s := testSession(t)
	items := []media.Item{{ID: "a", Type: "Audio"}, {ID: "b", Type: "Audio"}}
	s.applyRemoteItems(remote.Command{PlayMode: "now"}, items)
	s.handlePlayback(PlaybackEvent{Kind: PlaybackEnded, ID: 1})
	active := s.controller.active.id
	s.applyRemoteItems(remote.Command{PlayMode: "now", StartIndex: 1}, []media.Item{items[1], items[0]})
	if s.controller.active.id != active || s.playbackQueue.switching || s.controller.stoppedByUser {
		t.Fatal("queue reorder restarted the decoder")
	}
}

func TestLocalQueueKeepsSelectedLibraryTrack(t *testing.T) {
	s := testSession(t)
	items := []media.Item{{ID: "a", Type: "Audio"}, {ID: "b", Type: "Audio"}}
	s.controller.item = items[0]
	s.model.Stack = append(s.model.Stack, View{Page: media.Page{Items: items}}, View{Detail: &items[0]})
	s.adoptLocalQueue()
	s.moveQueue(1, false)
	s.handlePlayback(PlaybackEvent{Kind: PlaybackEnded, ID: 1})
	s.handleRemote(remote.Command{Kind: remote.Stop})
	s.handlePlayback(PlaybackEvent{Kind: PlaybackEnded, ID: 2})
	if s.model.Current().Selected != 1 {
		t.Fatal("returning to library lost track selection")
	}
}

func TestRemoteRepeatAdoptsWholeShuffleBatch(t *testing.T) {
	s := testSession(t)
	items := []media.Item{{ID: "a", Type: "Audio"}, {ID: "b", Type: "Audio"}, {ID: "c", Type: "Audio"}}
	s.controller.item = items[1]
	s.shuffle = shuffleQueue{library: "music", items: items, position: 1}
	s.handleRemote(remote.Command{Kind: remote.Repeat, Repeat: remote.RepeatAll})
	state := s.playbackQueue.queue.Snapshot()
	if len(state.Entries) != 3 || s.playbackQueue.queue.Current().ID != "b" || state.Repeat != remote.RepeatAll || !state.Shuffled {
		t.Fatal("shuffle adoption lost queue", state)
	}
}

func TestCatalogCancellationPreservesPlaybackAndRejectsLocalResult(t *testing.T) {
	s := testSession(t)
	s.controller.item = media.Item{ID: "current", Type: "Audio"}
	remoteCtx, cancelRemote := context.WithCancel(context.Background())
	localCtx, cancelLocal := context.WithCancel(context.Background())
	defer cancelRemote()
	defer cancelLocal()
	s.remoteRequests.cancel = cancelRemote
	s.remoteRequests.localCancel = cancelLocal
	s.remoteRequests.resolving = true
	s.remoteRequests.requests = []remote.Command{{Kind: remote.Play, PlayMode: remote.PlayNext}}
	s.remoteRequests.cancelAll()
	if remoteCtx.Err() == nil || localCtx.Err() == nil || s.remoteRequests.resolving || len(s.remoteRequests.requests) != 0 {
		t.Fatal("catalog work survived cancellation")
	}
	result := localQueueResult{generation: 0, itemID: "current", items: []media.Item{s.controller.item}}
	result.apply(s)
	if s.playbackQueue.active || !s.controller.running || s.controller.stoppedByUser {
		t.Fatal("stale local result changed playback or installed a queue")
	}
}

func TestRemoteAudioTargetBecomesRelativeDecoderOffset(t *testing.T) {
	f := newControllerFixture(t)
	f.c.item.Type = "Audio"
	f.c.state.PositionTicks = 60 * 10000000
	setControllerPaused(t, f.c, true, f.now)
	for _, tc := range []struct {
		target int64
		offset int
	}{{90 * 10000000, 30}, {23 * 10000000, -37}} {
		f.c.SeekTo(tc.target, f.now)
		select {
		case command := <-f.c.active.controls:
			if command.Kind != playback.SeekAudioRelative || command.Seconds != tc.offset {
				t.Fatal(command)
			}
		default:
			t.Fatal("remote audio seek was not sent")
		}
		if !f.c.state.Paused || len(f.calls) != 1 {
			t.Fatal("remote audio seek changed pause or reloaded media")
		}
	}
}

func TestRemotePauseDuringQueueHandoffUsesNextTrackIntent(t *testing.T) {
	for _, oldPaused := range []bool{false, true} {
		prefix := "from_playing/"
		if oldPaused {
			prefix = "from_paused/"
		}
		for _, tc := range []struct {
			name       string
			commands   []remote.Kind
			wantPaused bool
		}{
			{"pause_then_toggle", []remote.Kind{remote.Pause, remote.TogglePause}, false},
			{"two_toggles", []remote.Kind{remote.TogglePause, remote.TogglePause}, false},
			{"resume_then_toggle", []remote.Kind{remote.Resume, remote.TogglePause}, true},
			{"repeated_pause", []remote.Kind{remote.Pause, remote.Pause}, true},
			{"toggle", []remote.Kind{remote.TogglePause}, true},
		} {
			t.Run(prefix+tc.name, func(t *testing.T) {
				s := testSession(t)
				setControllerPaused(t, s.controller, oldPaused, time.Now())
				old := s.controller.active
				s.applyRemoteItems(remote.Command{PlayMode: remote.PlayNow}, []media.Item{{ID: "next", Type: "Audio"}})
				if !s.playbackQueue.switching {
					t.Fatal("test did not enter queue handoff")
				}
				for _, kind := range tc.commands {
					s.handleRemote(remote.Command{Kind: kind})
				}
				if len(old.controls) != 0 {
					t.Fatal("handoff commands reached the outgoing decoder")
				}
				s.handlePlayback(PlaybackEvent{Kind: PlaybackEnded, ID: old.id})
				s.handlePlayback(PlaybackEvent{Kind: PlaybackPosition, ID: s.controller.active.id, Ticks: 1})
				if s.controller.item.ID != "next" || s.controller.wantsPause() != tc.wantPaused {
					t.Fatalf("next item=%q paused=%v, want paused=%v", s.controller.item.ID, s.controller.wantsPause(), tc.wantPaused)
				}
				if tc.wantPaused {
					expectCommand(t, s.controller.active.controls, playback.SetPaused)
				}
				if len(s.controller.active.controls) != 0 {
					t.Fatal("replacement received unexpected controls")
				}
			})
		}
	}
}
