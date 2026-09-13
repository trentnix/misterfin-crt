package browser

import (
	"errors"
	"testing"
	"time"

	"misterfin-go/internal/jellyfin"
	"misterfin-go/internal/playback"
)

type controllerLaunch struct {
	tracks   playback.TrackOptions
	offset   *int64
	gate     <-chan struct{}
	controls chan playback.Control
	canceled bool
	cleanup  chan struct{}
}
type controllerFixture struct {
	c     *PlaybackController
	calls []*controllerLaunch
	now   time.Time
}

func newControllerFixture(t *testing.T) *controllerFixture {
	t.Helper()
	f := &controllerFixture{now: time.Unix(100, 0)}
	f.c = newPlaybackController(func(item jellyfin.Item, offset *int64, gate <-chan struct{}, prepared bool, controls chan playback.Control, tracks playback.TrackOptions) playbackProcess {
		call := &controllerLaunch{tracks: tracks, offset: offset, gate: gate, controls: controls, cleanup: make(chan struct{})}
		f.calls = append(f.calls, call)
		return playbackProcess{id: len(f.calls), cancel: func() { call.canceled = true }, cleanup: call.cleanup}
	})
	f.c.Start(jellyfin.Item{ID: "movie", Type: "Movie", Name: "Movie", RunTimeTicks: 6000000000}, nil, false, f.now)
	f.c.Handle(PlaybackEvent{Kind: PlaybackPosition, ID: 1, Ticks: 20000000}, f.now)
	return f
}
func expectCommand(t *testing.T, q chan playback.Control, kind string) {
	t.Helper()
	select {
	case got := <-q:
		if got.Kind != kind {
			t.Fatalf("command %q, want %q", got.Kind, kind)
		}
	default:
		t.Fatalf("missing %s command", kind)
	}
}

func TestStopDetachesReportsButWaitsForDecoder(t *testing.T) {
	f := newControllerFixture(t)
	f.c.Key("back", f.now)
	f.c.Key("back", f.now) // Repeated Stop must not close the cleanup signal twice.
	if !f.calls[0].canceled {
		t.Fatal("Stop did not cancel decoding")
	}
	select {
	case <-f.calls[0].cleanup:
	default:
		t.Fatal("Stop still waits for server reporting")
	}
	if !f.c.Snapshot(f.now).Active {
		t.Fatal("browser reclaimed output before the decoder exited")
	}
	if !f.c.Handle(PlaybackEvent{Kind: PlaybackEnded, ID: 1}, f.now) || f.c.Snapshot(f.now).Active {
		t.Fatal("decoder completion did not return to browsing")
	}
}

func TestImmediateReopenUsesPositionPendingSave(t *testing.T) {
	for _, saved := range []bool{false, true} {
		f := newControllerFixture(t)
		f.c.Key("back", f.now)
		f.c.Handle(PlaybackEvent{Kind: PlaybackEnded, ID: 1}, f.now)
		if saved {
			f.c.Handle(PlaybackEvent{Kind: PlaybackCleanupDone, ID: 1}, f.now)
		}
		f.c.Start(f.c.item, nil, false, f.now)
		offset := f.calls[1].offset
		if saved && offset != nil {
			t.Fatal("completed cleanup should allow the normal server resume lookup")
		}
		if !saved && (offset == nil || *offset != 20000000) {
			t.Fatal("immediate reopen lost the position still being saved")
		}
		f.c.Handle(PlaybackEvent{Kind: PlaybackCleanupDone, ID: 1}, f.now)
		if f.c.cleanupComplete {
			t.Fatal("old cleanup changed the new session")
		}
	}
}

func TestFirstVideoFrameClearsLoadingBeforePosition(t *testing.T) {
	f := newControllerFixture(t)
	c := f.c
	c.Start(jellyfin.Item{ID: "movie", Type: "Movie"}, nil, false, f.now)
	c.Key("controls", f.now)
	c.Handle(PlaybackEvent{Kind: PlaybackVideoStarted, ID: 1}, f.now)
	if c.Snapshot(f.now).WaitLabel != "Loading..." {
		t.Fatal("stale decoder cleared loading")
	}
	later := f.now.Add(5 * time.Second)
	c.Handle(PlaybackEvent{Kind: PlaybackVideoStarted, ID: 2}, later)
	if c.Snapshot(later).WaitLabel != "" || c.state.ProgressSeen {
		t.Fatal("first frame did not clear loading independently of position")
	}
	if c.Snapshot(later.Add(3 * time.Second)).ControlsVisible {
		t.Fatal("first frame left the loading menu pinned")
	}
	if c.Snapshot(later.Add(3*time.Second)).WaitLabel != "Buffering..." {
		t.Fatal("first frame disabled subsequent stall detection")
	}
	c.Handle(PlaybackEvent{Kind: PlaybackPosition, ID: 2, Ticks: 10000000}, later)
	c.Key("seek-forward", later)
	c.Tick(later.Add(time.Second))
	c.Handle(PlaybackEvent{Kind: PlaybackPrepared, ID: 3}, later)
	c.Handle(PlaybackEvent{Kind: PlaybackEnded, ID: 2}, later)
	if c.Snapshot(later).WaitLabel != "Loading..." {
		t.Fatal("replacement decoder retained the old first-frame state")
	}
	c.Handle(PlaybackEvent{Kind: PlaybackVideoStarted, ID: 2}, later)
	if c.Snapshot(later).WaitLabel != "Loading..." {
		t.Fatal("old decoder cleared replacement loading")
	}
	c.Handle(PlaybackEvent{Kind: PlaybackVideoStarted, ID: 3}, later)
	if c.Snapshot(later).WaitLabel != "" {
		t.Fatal("replacement's first frame did not clear loading")
	}
}
func TestControllerSeekRetargetDuringHandoff(t *testing.T) {
	f := newControllerFixture(t)
	c := f.c
	c.Key("seek-forward", f.now)
	c.Key("seek-forward", f.now)
	preview := c.Snapshot(f.now)
	if !preview.ShowDestination || preview.DestinationTicks != 620000000 {
		t.Fatal(preview)
	}
	c.Tick(f.now.Add(499 * time.Millisecond))
	if len(f.calls) != 1 {
		t.Fatal("seek launched before deadline")
	}
	c.Tick(f.now.Add(500 * time.Millisecond))
	expectCommand(t, f.calls[0].controls, "pause")
	if c.Snapshot(f.now).WaitLabel != "Seeking..." || *f.calls[1].offset != 620000000 {
		t.Fatal("missing seek")
	}
	c.Handle(PlaybackEvent{Kind: PlaybackPaused, ID: 1, Value: true}, f.now)
	c.Handle(PlaybackEvent{Kind: PlaybackPrepared, ID: 2}, f.now)
	if !f.calls[0].canceled {
		t.Fatal("old decoder not stopped after preparation")
	}
	c.Key("seek-forward", f.now.Add(time.Second))
	if !f.calls[1].canceled || c.Snapshot(f.now).DestinationTicks != 920000000 || !c.Snapshot(f.now).ShowDestination {
		t.Fatal("retarget did not restore destination")
	}
	// The old decoder finishes while the new destination is still debouncing.
	if c.Handle(PlaybackEvent{Kind: PlaybackEnded, ID: 1}, f.now) {
		t.Fatal("handoff ended item")
	}
	c.Handle(PlaybackEvent{Kind: PlaybackEnded, ID: 2}, f.now) // obsolete replacement
	c.Handle(PlaybackEvent{Kind: PlaybackPrepared, ID: 2}, f.now)
	c.Tick(f.now.Add(1499 * time.Millisecond))
	if len(f.calls) != 2 {
		t.Fatal("retarget ignored debounce")
	}
	c.Tick(f.now.Add(1500 * time.Millisecond))
	if *f.calls[1].offset != 620000000 || *f.calls[2].offset != 920000000 {
		t.Fatal("request offsets were mutated")
	}
	c.Handle(PlaybackEvent{Kind: PlaybackPrepared, ID: 3}, f.now)
	select {
	case <-f.calls[2].gate:
	default:
		t.Fatal("replacement gate did not open")
	}
	c.Handle(PlaybackEvent{Kind: PlaybackPosition, ID: 3, Ticks: 940000000}, f.now)
	p := c.Snapshot(f.now)
	if p.PositionTicks != 940000000 || p.WaitLabel != "" || p.HasDestination {
		t.Fatal(p)
	}
	if preview.DestinationTicks != 620000000 {
		t.Fatal("old snapshot changed")
	}
}
func TestControllerPreservesPauseAndRejectsStaleEvents(t *testing.T) {
	f := newControllerFixture(t)
	c := f.c
	c.Key("open", f.now)
	expectCommand(t, f.calls[0].controls, "pause")
	c.Handle(PlaybackEvent{Kind: PlaybackPaused, ID: 1, Value: true}, f.now)
	c.Key("seek-forward", f.now)
	c.Tick(f.now.Add(time.Second))
	if len(f.calls[0].controls) != 0 {
		t.Fatal("seek toggled existing pause")
	}
	c.Handle(PlaybackEvent{Kind: PlaybackPrepared, ID: 2}, f.now)
	c.Handle(PlaybackEvent{Kind: PlaybackEnded, ID: 1}, f.now)
	c.Handle(PlaybackEvent{Kind: PlaybackPosition, ID: 2, Ticks: 340000000}, f.now)
	expectCommand(t, f.calls[1].controls, "pause")
	c.Handle(PlaybackEvent{Kind: PlaybackPaused, ID: 2, Value: true}, f.now)
	c.Handle(PlaybackEvent{Kind: PlaybackPosition, ID: 1, Ticks: 0}, f.now)
	c.Handle(PlaybackEvent{Kind: PlaybackPaused, ID: 1, Value: false}, f.now)
	p := c.Snapshot(f.now)
	if !p.Paused || p.PositionTicks != 340000000 {
		t.Fatal(p)
	}
	c.Key("controls", f.now)
	if !c.Snapshot(f.now).ControlsVisible || c.Snapshot(f.now.Add(3*time.Second)).ControlsVisible {
		t.Fatal("control reveal duration changed")
	}
	c.Key("open", f.now)
	if c.Snapshot(f.now).ControlsVisible {
		t.Fatal("pause toggle left controls visible")
	}
}
func TestControllerSeekFailureAndStop(t *testing.T) {
	f := newControllerFixture(t)
	c := f.c
	c.Key("seek-forward", f.now)
	c.Tick(f.now.Add(time.Second))
	expectCommand(t, f.calls[0].controls, "pause")
	c.Handle(PlaybackEvent{Kind: PlaybackPaused, ID: 1, Value: true}, f.now)
	if c.Handle(PlaybackEvent{Kind: PlaybackEnded, ID: 2, Err: errors.New("stream failed")}, f.now) {
		t.Fatal("failed seek ended original item")
	}
	expectCommand(t, f.calls[0].controls, "pause")
	p := c.Snapshot(f.now)
	if p.Notice == "" || p.HasDestination || !c.running {
		t.Fatal(p)
	}
	c.Key("seek-forward", f.now)
	c.Tick(f.now.Add(time.Second))
	c.Key("back", f.now)
	if !f.calls[0].canceled || !f.calls[2].canceled {
		t.Fatal("stop did not cancel both processes")
	}
	if c.Handle(PlaybackEvent{Kind: PlaybackEnded, ID: 3}, f.now) {
		t.Fatal("canceled replacement ended item")
	}
	if !c.Handle(PlaybackEvent{Kind: PlaybackEnded, ID: 1}, f.now) || c.running {
		t.Fatal("stop did not finish item")
	}
}
func TestControllerStopWhileRetargetWaitsWithoutDecoder(t *testing.T) {
	f := newControllerFixture(t)
	c := f.c
	c.Key("seek-forward", f.now)
	c.Tick(f.now.Add(time.Second))
	c.Handle(PlaybackEvent{Kind: PlaybackPrepared, ID: 2}, f.now)
	c.Key("seek-forward", f.now)
	c.Handle(PlaybackEvent{Kind: PlaybackEnded, ID: 1}, f.now)
	c.Key("back", f.now)
	c.Tick(f.now.Add(time.Second))
	if c.running || len(f.calls) != 2 {
		t.Fatal("stop launched another seek")
	}
}
func TestControllerLiveTVDoesNotSeek(t *testing.T) {
	f := newControllerFixture(t)
	f.c.item.Type = "TvChannel"
	f.c.Key("seek-forward", f.now)
	f.c.Key("seek-backward", f.now)
	f.c.Tick(f.now.Add(time.Second))
	p := f.c.Snapshot(f.now)
	if p.Seekable || p.HasDestination || len(f.calls) != 1 {
		t.Fatal(p)
	}
}

func TestControllerSeekFailureAfterOriginalEnded(t *testing.T) {
	f := newControllerFixture(t)
	c := f.c
	c.Key("seek-forward", f.now)
	c.Tick(f.now.Add(time.Second))
	c.Handle(PlaybackEvent{Kind: PlaybackPrepared, ID: 2}, f.now)
	c.Key("seek-forward", f.now.Add(time.Second))
	c.Handle(PlaybackEvent{Kind: PlaybackEnded, ID: 1}, f.now)
	c.Tick(f.now.Add(1500 * time.Millisecond))

	// The original has released its output. A failed replacement cannot resume it.
	if c.Handle(PlaybackEvent{Kind: PlaybackEnded, ID: 3, Err: errors.New("stream failed")}, f.now) {
		t.Fatal("failed replacement reported normal item completion")
	}
	p := c.Snapshot(f.now)
	if c.running || c.state.PlayingVideo || p.Paused || p.HasDestination || p.Notice == "" {
		t.Fatalf("failed replacement left playback active: %+v", p)
	}
	c.Tick(f.now.Add(2 * time.Second))
	if len(f.calls) != 3 {
		t.Fatal("failed replacement was relaunched")
	}
}

func TestMenuSeekStaysVisibleThroughRetargetAndStartup(t *testing.T) {
	f := newControllerFixture(t)
	c := f.c
	c.Key("controls", f.now)
	c.Key("seek-forward", f.now)
	c.Key("seek-forward", f.now.Add(100*time.Millisecond))
	c.Tick(f.now.Add(time.Second))
	later := f.now.Add(10 * time.Second)
	if !c.Snapshot(later).ControlsVisible {
		t.Fatal("menu expired during seek preparation")
	}
	c.Key("seek-backward", later)
	if p := c.Snapshot(later); !p.ControlsVisible || !p.ShowDestination {
		t.Fatalf("retarget lost menu preview: %+v", p)
	}
	c.Tick(later.Add(time.Second))
	c.Handle(PlaybackEvent{Kind: PlaybackPrepared, ID: 3}, later)
	c.Handle(PlaybackEvent{Kind: PlaybackEnded, ID: 1}, later)
	ready := later.Add(10 * time.Second)
	if p := c.Snapshot(ready); !p.ControlsVisible || p.WaitLabel != "Loading..." {
		t.Fatalf("handoff lost menu: %+v", p)
	}
	c.Handle(PlaybackEvent{Kind: PlaybackPosition, ID: 3, Ticks: 320000000}, ready)
	if !c.Snapshot(ready.Add(2*time.Second)).ControlsVisible || c.Snapshot(ready.Add(3*time.Second)).ControlsVisible {
		t.Fatal("menu timeout did not restart after seek playback began")
	}
	c.Key("seek-forward", ready.Add(2*time.Second))
	if !c.Snapshot(ready.Add(8 * time.Second)).ControlsVisible {
		t.Fatal("subsequent seek did not retain the open menu")
	}
	c.Key("back", ready.Add(9*time.Second))
	if c.Snapshot(ready.Add(9 * time.Second)).ControlsVisible {
		t.Fatal("stopping retained seek menu")
	}
}

func TestFailedSeekReleasesMenuTimeout(t *testing.T) {
	f := newControllerFixture(t)
	c := f.c
	c.Key("controls", f.now)
	c.Key("seek-forward", f.now)
	c.Tick(f.now.Add(time.Second))
	failed := f.now.Add(10 * time.Second)
	c.Handle(PlaybackEvent{Kind: PlaybackEnded, ID: 2, Err: errors.New("failed")}, failed)
	if !c.Snapshot(failed).ControlsVisible || c.Snapshot(failed.Add(3*time.Second)).ControlsVisible {
		t.Fatal("failed seek left the menu pinned")
	}
}

func TestUpTogglesControlsDuringPlaybackAndSeek(t *testing.T) {
	f := newControllerFixture(t)
	c := f.c
	c.Key("controls", f.now)
	if !c.state.ControlsVisible(f.now) {
		t.Fatal("Up did not reveal controls")
	}
	c.Key("controls", f.now)
	if c.state.ControlsVisible(f.now) {
		t.Fatal("second Up did not hide controls")
	}
	c.Key("controls", f.now)
	c.Key("seek-forward", f.now)
	c.Tick(f.now.Add(time.Second))
	c.Key("controls", f.now.Add(time.Second))
	if c.state.ControlsVisible(f.now.Add(time.Second)) {
		t.Fatal("Up did not hide pinned seek controls")
	}
	c.Key("controls", f.now.Add(time.Second))
	if !c.state.ControlsVisible(f.now.Add(10 * time.Second)) {
		t.Fatal("Up did not reveal and pin seeking controls")
	}
}

func TestUpTogglesMusicControls(t *testing.T) {
	f := newControllerFixture(t)
	f.c.Start(jellyfin.Item{Type: "Audio"}, nil, false, f.now)
	f.c.Key("controls", f.now)
	if !f.c.Snapshot(f.now).ControlsVisible {
		t.Fatal("controls not revealed")
	}
	f.c.Key("controls", f.now)
	if f.c.Snapshot(f.now).ControlsVisible {
		t.Fatal("controls not dismissed")
	}
}
