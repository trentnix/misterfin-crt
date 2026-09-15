package browser

import (
	"testing"

	"misterfin-crt/internal/input/control"
	"misterfin-crt/internal/jellyfin"
)

func TestLiveCaptionsToggleWithoutRestartAndKeepMenuState(t *testing.T) {
	f := newControllerFixture(t)
	c := f.c
	c.item.Type = "TvChannel"
	caption := func(id int, text string) {
		c.Handle(PlaybackEvent{Kind: PlaybackCaption, ID: id, Caption: text}, f.now)
	}
	caption(1, "First line")
	if got := c.Snapshot(f.now).Subtitle; got != "" {
		t.Fatal("captions started enabled")
	}
	c.Key(control.Select, f.now)
	c.Key(control.Down, f.now)
	c.Key(control.Open, f.now)
	if got := c.Snapshot(f.now); got.Subtitle != "First line" || got.Tracks != nil || got.ControlsVisible {
		t.Fatal("enabling captions changed menu or lost current text")
	}
	c.Key(control.Select, f.now)
	caption(1, "Second line")
	if got := c.Snapshot(f.now); got.Subtitle != "Second line" || got.Tracks == nil {
		t.Fatal("caption update dismissed picker")
	}
	caption(99, "Stale")
	if c.Snapshot(f.now).Subtitle != "Second line" {
		t.Fatal("stale decoder replaced caption")
	}
	caption(1, "")
	if c.Snapshot(f.now).Subtitle != "" {
		t.Fatal("caption clear ignored")
	}
	c.Key(control.Up, f.now)
	c.Key(control.Open, f.now)
	caption(1, "Hidden")
	if c.Snapshot(f.now).Subtitle != "" {
		t.Fatal("Off did not hide later captions")
	}
	if len(f.calls) != 1 || len(c.controls) != 0 || c.state.SeekTarget != nil {
		t.Fatal("caption toggle interrupted stream")
	}
	c.Handle(PlaybackEvent{Kind: PlaybackEnded, ID: 1}, f.now)
	c.Start(jellyfin.Item{ID: "next-channel", Type: "TvChannel"}, nil, false, f.now)
	if c.captions.available || c.captions.enabled || c.captions.text != "" {
		t.Fatal("new channel inherited captions")
	}
}
