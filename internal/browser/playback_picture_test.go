package browser

import (
	"errors"
	"testing"
	"time"

	"misterfin-go/internal/playback"
)

func selectPicture(c *PlaybackController, now time.Time, mode playback.PictureMode) {
	c.Key("select", now)
	// Use navigation rather than mutating the picker so tab routing is covered.
	c.Key("next", now)
	c.Key("next", now)
	if mode == playback.PictureZoom43 {
		c.Key("down", now)
	} else {
		c.Key("up", now)
	}
	c.Key("open", now)
}

func TestPictureChangePreservesPositionPauseTracksAndSeeks(t *testing.T) {
	for _, paused := range []bool{false, true} {
		f := trackFixture(t)
		c := f.c
		c.state.Paused = paused
		c.tracks.Selection.AudioIndex = 8
		c.trackOptions = c.tracks.TrackOptions
		selectPicture(c, f.now, playback.PictureZoom43)
		if len(f.calls) != 2 || *f.calls[1].offset != 20000000 || f.calls[1].tracks.Picture != playback.PictureZoom43 || f.calls[1].tracks.Selection.AudioIndex != 8 {
			t.Fatal("picture change lost position, mode, or audio selection")
		}
		if c.Snapshot(f.now).WaitLabel != "Loading..." {
			t.Fatal("picture change was labeled as a seek")
		}
		if !paused {
			expectCommand(t, f.calls[0].controls, "pause")
		}
		info := c.tracks
		info.TrackOptions = f.calls[1].tracks
		c.Handle(PlaybackEvent{Kind: PlaybackTrackInfo, ID: 2, Tracks: info}, f.now)
		c.Handle(PlaybackEvent{Kind: PlaybackPrepared, ID: 2}, f.now)
		c.Handle(PlaybackEvent{Kind: PlaybackEnded, ID: 1}, f.now)
		c.Handle(PlaybackEvent{Kind: PlaybackPosition, ID: 2, Ticks: 20000000}, f.now)
		if paused {
			expectCommand(t, f.calls[1].controls, "pause")
		}
		if !c.trackRows(2)[1].Active {
			t.Fatal("Zoom is not marked active")
		}
		if c.picker.visible || c.state.ControlsVisible(f.now) {
			t.Fatal("picture handoff left a menu visible")
		}
		c.Key("seek-forward", f.now)
		c.Tick(f.now.Add(time.Second))
		if f.calls[2].tracks.Picture != playback.PictureZoom43 {
			t.Fatal("seek lost picture mode")
		}
	}
}

func TestFailedPictureChangeRestoresOriginal(t *testing.T) {
	f := trackFixture(t)
	selectPicture(f.c, f.now, playback.PictureZoom43)
	expectCommand(t, f.calls[0].controls, "pause")
	f.c.Handle(PlaybackEvent{Kind: PlaybackEnded, ID: 2, Err: errors.New("cannot prepare")}, f.now)
	expectCommand(t, f.calls[0].controls, "pause")
	if !f.c.running || f.calls[0].canceled || f.c.trackOptions.Picture != playback.PictureOriginal || !f.c.trackRows(2)[0].Active {
		t.Fatal("failed picture change did not retain Original")
	}
}

func TestPictureMenuWithoutAlternateTracksAndRestoreOriginal(t *testing.T) {
	f := newControllerFixture(t)
	if !f.c.Snapshot(f.now).TracksAvailable {
		t.Fatal("video without alternate tracks has no picture options")
	}
	selectPicture(f.c, f.now, playback.PictureOriginal)
	if len(f.calls) != 1 || f.c.picker.visible {
		t.Fatal("reselecting Original did not dismiss the menu without restarting")
	}
	f.c.tracks.Picture = playback.PictureZoom43
	f.c.trackOptions.Picture = playback.PictureZoom43
	selectPicture(f.c, f.now, playback.PictureOriginal)
	if len(f.calls) != 2 || f.calls[1].tracks.Picture != playback.PictureOriginal {
		t.Fatal("cannot restore Original")
	}
}

func TestLivePictureChangesDismissMenuAndKeepFrameAndDecoder(t *testing.T) {
	for _, paused := range []bool{false, true} {
		f := trackFixture(t)
		c := f.c
		c.tracks.LivePicture = true
		c.state.Paused = paused
		before := c.state.PositionTicks
		selectPicture(c, f.now, playback.PictureZoom43)
		first := <-c.controls
		if first.Kind != "picture" || first.Picture != playback.PictureZoom43 || len(f.calls) != 1 || !c.picker.visible || c.state.SeekTarget != nil {
			t.Fatal("picture change restarted or hid menu")
		}
		// Return to Original before the first acknowledgment arrives.
		c.Key("up", f.now)
		c.Key("open", f.now)
		second := <-c.controls
		if second.Picture != playback.PictureOriginal || second.Request <= first.Request {
			t.Fatal("cannot retarget live picture change")
		}
		c.Handle(PlaybackEvent{Kind: PlaybackPicture, ID: 1, Picture: playback.PictureResult{Request: first.Request, Mode: playback.PictureZoom43}}, f.now)
		if !c.picturePending || c.tracks.Picture != playback.PictureOriginal {
			t.Fatal("stale reply applied")
		}
		c.Handle(PlaybackEvent{Kind: PlaybackPicture, ID: 1, Picture: playback.PictureResult{Request: second.Request, Mode: playback.PictureOriginal}}, f.now)
		if c.picturePending || c.picker.visible || c.state.ControlsVisible(f.now) || c.state.Paused != paused || c.state.PositionTicks != before || len(f.calls) != 1 {
			t.Fatal("picture command changed playback or menu")
		}
		c.Key("select", f.now)
		c.Key("down", f.now)
		c.Key("open", f.now)
		third := <-c.controls
		c.Handle(PlaybackEvent{Kind: PlaybackPicture, ID: 1, Picture: playback.PictureResult{Request: third.Request, Mode: playback.PictureZoom43}}, f.now)
		if !c.trackRows(2)[1].Active {
			t.Fatal("successful zoom not active")
		}
		c.Key("select", f.now)
		c.Key("open", f.now) // Applying the active choice also dismisses the picker.
		if c.picker.visible || len(c.controls) != 0 {
			t.Fatal("reselecting live Zoom left the picker open or sent another command")
		}
		c.Key("seek-forward", f.now)
		c.Tick(f.now.Add(time.Second))
		if len(f.calls) != 2 || f.calls[1].tracks.Picture != playback.PictureZoom43 {
			t.Fatal("seek lost live choice")
		}
	}
}

func TestNativePictureFailureKeepsMenuAndOriginal(t *testing.T) {
	f := trackFixture(t)
	f.c.tracks.LivePicture = true
	selectPicture(f.c, f.now, playback.PictureZoom43)
	request := <-f.c.controls
	f.c.Handle(PlaybackEvent{Kind: PlaybackPicture, ID: 1, Picture: playback.PictureResult{Request: request.Request, Err: errors.New("cannot change picture mode")}}, f.now)
	if !f.c.picker.visible || f.c.picturePending || f.c.trackOptions.Picture != playback.PictureOriginal || f.c.notice == "" {
		t.Fatal("failed picture request lost menu or original mode")
	}
}
