package browser

import (
	"errors"
	"testing"
	"time"

	"misterfin-crt/internal/jellyfin"
	"misterfin-crt/internal/playback"
	"misterfin-crt/internal/subtitles"
)

func trackFixture(t *testing.T) *controllerFixture {
	f := newControllerFixture(t)
	f.c.Handle(PlaybackEvent{Kind: PlaybackTrackInfo, ID: 1, Tracks: playback.VideoTracks{ClientSubtitles: true, SourceID: "source", TrackOptions: playback.TrackOptions{Selection: jellyfin.TrackSelection{AudioIndex: -1, SubtitleIndex: -1}}, Streams: []jellyfin.MediaStream{{Type: "Audio", Index: 3, DisplayTitle: "Japanese"}, {Type: "Audio", Index: 8, DisplayTitle: "English"}, {Type: "Subtitle", Index: 12, Codec: "ass", DisplayTitle: "English text"}, {Type: "Subtitle", Index: 20, Codec: "pgssub", DisplayTitle: "English PGS"}}}}, f.now)
	return f
}
func TestTextSubtitleSelectionDoesNotRestartVideo(t *testing.T) {
	f := trackFixture(t)
	c := f.c
	c.Key("select", f.now)
	c.Key("down", f.now)
	c.Key("open", f.now)
	command := <-f.calls[0].controls
	if command.Kind != "subtitle" || command.Index != 12 || len(f.calls) != 1 {
		t.Fatal("text subtitle restarted video or used row index")
	}
	text, _ := subtitles.Parse([]byte("1\n00:00:01,000 --> 00:00:03,000\nHello"))
	c.Handle(PlaybackEvent{Kind: PlaybackSubtitle, ID: 1, Subtitle: playback.SubtitleResult{Index: 12, Text: text, Request: c.subtitleRequest}}, f.now)
	if got := c.Snapshot(f.now); got.Subtitle != "Hello" || got.Tracks != nil {
		t.Fatal("successful selection did not show text and close menu")
	}
	c.Handle(PlaybackEvent{Kind: PlaybackSubtitle, ID: 99, Subtitle: playback.SubtitleResult{Index: -1}}, f.now)
	if c.Snapshot(f.now).Subtitle != "Hello" {
		t.Fatal("stale decoder removed subtitles")
	}
	c.Handle(PlaybackEvent{Kind: PlaybackSubtitle, ID: 1, Subtitle: playback.SubtitleResult{Err: errors.New("download failed")}}, f.now)
	if c.Snapshot(f.now).Subtitle != "Hello" {
		t.Fatal("failed download removed preceding subtitle")
	}
}
func TestAudioSelectionPreservesPauseOffsetAndFutureSeeks(t *testing.T) {
	for _, paused := range []bool{false, true} {
		f := trackFixture(t)
		c := f.c
		c.state.Paused = paused
		c.Key("select", f.now)
		c.Key("next", f.now)
		c.Key("down", f.now)
		c.Key("down", f.now)
		c.Key("open", f.now)
		if c.Snapshot(f.now).WaitLabel != "Loading..." {
			t.Fatal("track change was labeled as a seek")
		}
		if len(f.calls) != 2 || *f.calls[1].offset != 20000000 || f.calls[1].tracks.Selection.AudioIndex != 8 {
			t.Fatal("audio switch lost position or stream index")
		}
		if !paused {
			expectCommand(t, f.calls[0].controls, "pause")
		}
		info := c.tracks
		info.TrackOptions = f.calls[1].tracks
		c.Handle(PlaybackEvent{Kind: PlaybackTrackInfo, ID: 2, Tracks: info}, f.now)
		c.Handle(PlaybackEvent{Kind: PlaybackPrepared, ID: 2}, f.now)
		if !f.calls[0].canceled {
			t.Fatal("old decoder was not stopped after replacement prepared")
		}
		c.Handle(PlaybackEvent{Kind: PlaybackEnded, ID: 1}, f.now)
		c.Handle(PlaybackEvent{Kind: PlaybackPosition, ID: 2, Ticks: 20000000}, f.now)
		if paused {
			expectCommand(t, f.calls[1].controls, "pause")
		}
		c.Key("seek-forward", f.now)
		c.Tick(f.now.Add(time.Second))
		if f.calls[2].tracks.Selection.AudioIndex != 8 {
			t.Fatal("seeking lost selected audio")
		}
	}
}
func TestImageSubtitleAndCompanionTextRestartStream(t *testing.T) {
	for _, soft := range []bool{true, false} {
		f := trackFixture(t)
		f.c.tracks.ClientSubtitles = soft
		f.c.Key("select", f.now)
		f.c.Key("down", f.now)
		want := 12
		if soft {
			f.c.Key("down", f.now)
			want = 20
		}
		f.c.Key("open", f.now)
		if len(f.calls) != 2 || f.calls[1].tracks.Selection.SubtitleIndex != want {
			t.Fatal("subtitle burn-in did not request replacement")
		}
	}
}
func TestPickerBackAndLiveTV(t *testing.T) {
	f := trackFixture(t)
	f.c.Key("select", f.now)
	f.c.Key("back", f.now)
	if !f.c.running || f.calls[0].canceled || f.c.picker.visible {
		t.Fatal("closing picker stopped playback")
	}
	f.c.item.Type = "TvChannel"
	f.c.Key("select", f.now)
	if f.c.picker.visible || f.c.Snapshot(f.now).TracksAvailable {
		t.Fatal("Live TV advertised unsupported picker")
	}
}

func TestFailedAudioChangeKeepsOriginalSelection(t *testing.T) {
	f := trackFixture(t)
	c := f.c
	c.Key("select", f.now)
	c.Key("next", f.now)
	c.Key("down", f.now)
	c.Key("open", f.now)
	expectCommand(t, f.calls[0].controls, "pause")
	c.Handle(PlaybackEvent{Kind: PlaybackEnded, ID: 2, Err: errors.New("cannot prepare")}, f.now)
	expectCommand(t, f.calls[0].controls, "pause")
	if !c.running || c.trackOptions.Selection.AudioIndex != -1 || f.calls[0].canceled {
		t.Fatal("failed handoff did not preserve old stream")
	}
}
func TestSubtitleDelayAndPause(t *testing.T) {
	f := trackFixture(t)
	c := f.c
	text, _ := subtitles.Parse([]byte("1\n00:00:02,000 --> 00:00:02,400\nHello"))
	c.Handle(PlaybackEvent{Kind: PlaybackSubtitle, ID: 1, Subtitle: playback.SubtitleResult{Index: 12, Text: text, Request: c.subtitleRequest}}, f.now)
	if c.Snapshot(f.now.Add(500*time.Millisecond)).Subtitle != "" {
		t.Fatal("subtitle clock did not advance")
	}
	c.state.Paused = true
	if c.Snapshot(f.now.Add(time.Second)).Subtitle != "Hello" {
		t.Fatal("paused subtitle advanced")
	}
	c.Key("select", f.now)
	c.Key("seek-forward", f.now)
	if c.subtitleDelay != 100*time.Millisecond || c.state.SeekTarget != nil {
		t.Fatal("subtitle timing control sought video")
	}
	if c.Snapshot(f.now).Subtitle != "" {
		t.Fatal("positive delay did not postpone cue")
	}
}

func TestSessionDirectionsNavigateOnlyWhilePickerOpen(t *testing.T) {
	s := testSession(t)
	f := trackFixture(t)
	s.controller = f.c
	s.handleKey("select")
	s.handleKey("down")
	s.handleKey("down-repeat")
	if !f.c.picker.visible || f.c.picker.selected[0] != 2 {
		t.Fatal("picker directions toggled controls instead of moving")
	}
	s.handleKey("next")
	if f.c.picker.tab != 1 {
		t.Fatal("Right did not select audio tab")
	}
	s.handleKey("back")
	if !f.c.running || f.c.picker.visible {
		t.Fatal("Back stopped instead of closing picker")
	}
	s.handleKey("up")
	if !f.c.state.ControlsVisible(time.Now()) {
		t.Fatal("normal directional menu toggle did not return")
	}
}

func TestOffCancelsPendingSubtitleAndRejectsQueuedReply(t *testing.T) {
	f := trackFixture(t)
	c := f.c
	c.Key("select", f.now)
	c.Key("down", f.now)
	c.Key("open", f.now)
	first := <-c.controls
	c.Key("up", f.now)
	c.Key("open", f.now)
	second := <-c.controls
	if second.Index != -1 || second.Request <= first.Request {
		t.Fatal("Off did not cancel pending text")
	}
	text, _ := subtitles.Parse([]byte("1\n00:00:01,000 --> 00:00:03,000\nOld"))
	c.Handle(PlaybackEvent{Kind: PlaybackSubtitle, ID: 1, Subtitle: playback.SubtitleResult{Index: 12, Text: text, Request: first.Request}}, f.now)
	if c.tracks.Text != nil {
		t.Fatal("queued stale extraction overrode Off")
	}
	c.Handle(PlaybackEvent{Kind: PlaybackSubtitle, ID: 1, Subtitle: playback.SubtitleResult{Index: -1, Request: second.Request}}, f.now)
	if c.subtitleLoading || c.tracks.Selection.SubtitleIndex != -1 {
		t.Fatal("Off did not settle")
	}
}

func TestViewNavigationAndBackDoNotRevealPlaybackControls(t *testing.T) {
	for _, paused := range []bool{false, true} {
		for _, visible := range []bool{false, true} {
			for _, closeKey := range []string{"back", "select"} {
				for tab := 0; tab < 3; tab++ {
					f := trackFixture(t)
					c := f.c
					c.state.Paused = paused
					if visible {
						c.state.RevealControls(f.now)
					}
					c.Key("select", f.now)
					for n := 0; n < tab; n++ {
						c.Key("next", f.now)
					}
					c.Key("down", f.now)
					c.Key("up", f.now)
					if c.Snapshot(f.now).ControlsVisible {
						t.Fatal("View navigation activated playback controls")
					}
					c.Key(closeKey, f.now)
					p := c.Snapshot(f.now)
					if p.Tracks != nil || p.ControlsVisible || !c.running || c.state.Paused != paused || f.calls[0].canceled {
						t.Fatal("View dismissal changed playback or revealed controls")
					}
				}
			}
		}
	}
}

func TestSubtitleCompletionAfterBackLeavesControlsAlone(t *testing.T) {
	for _, reopen := range []bool{false, true} {
		f := trackFixture(t)
		c := f.c
		c.Key("select", f.now)
		c.Key("down", f.now)
		c.Key("open", f.now)
		request := <-c.controls
		c.Key("back", f.now)
		if reopen {
			c.Key("controls", f.now)
		}
		c.Handle(PlaybackEvent{Kind: PlaybackSubtitle, ID: 1, Subtitle: playback.SubtitleResult{Request: request.Request, Index: 12}}, f.now)
		if p := c.Snapshot(f.now); p.Tracks != nil || p.ControlsVisible != reopen {
			t.Fatal("late subtitle completion changed overlay visibility")
		}
	}
}

func TestPictureMenuOffersZoomForEveryRecordedAspect(t *testing.T) {
	for _, aspect := range []string{"16:9", "235:100", "4:3", "1:1"} {
		t.Run(aspect, func(t *testing.T) {
			f := trackFixture(t)
			f.c.tracks.Streams = []jellyfin.MediaStream{{Type: "Video", Width: 720, Height: 480, AspectRatio: aspect}}
			f.c.Key("select", f.now)
			f.c.Key("next", f.now)
			f.c.Key("next", f.now)
			f.c.Key("down", f.now)
			menu := f.c.Snapshot(f.now).Tracks
			if menu == nil || menu.Tab != 2 || len(menu.Rows) != 2 || menu.Selected != 1 ||
				!menu.Rows[0].Active || menu.Rows[1].Index != int(playback.PictureZoom43) {
				t.Fatalf("aspect %s did not offer both picture modes: %+v", aspect, menu)
			}
			if menu.Message != "Enlarge the picture and crop the edges." {
				t.Fatal("Zoom description does not explain its general behavior")
			}
		})
	}
}

func TestPictureMenuKeepsZoomWhenSourceMetadataChanges(t *testing.T) {
	f := trackFixture(t)
	f.c.Key("select", f.now)
	f.c.Key("next", f.now)
	f.c.Key("next", f.now)
	f.c.Key("down", f.now)
	info := f.c.tracks
	info.Streams = []jellyfin.MediaStream{{Type: "Video", AspectRatio: "4:3"}}
	f.c.Handle(PlaybackEvent{Kind: PlaybackTrackInfo, ID: 1, Tracks: info}, f.now)
	menu := f.c.Snapshot(f.now).Tracks
	if menu.Selected != 1 || len(menu.Rows) != 2 {
		t.Fatal("source metadata changed the uniform picture choices")
	}
}
