package browser

import (
	"context"
	"testing"
	"time"

	"misterfin-crt/internal/input/control"
	"misterfin-crt/internal/jellyfin"
	"misterfin-crt/internal/release"
)

func TestAboutPreservesBrowseAndIsolatesInput(t *testing.T) {
	s := testSession(t)
	s.controller.running = false
	s.model.Current().Selected = 3
	s.model.Notice = "Existing notice"
	for _, key := range []control.Action{"about", "about-repeat", "next", "open"} {
		s.handleKey(key)
	}
	if !s.about.Visible || s.model.Current().Selected != 3 || s.model.Notice != "Existing notice" {
		t.Fatal("About leaked input to browser")
	}
	s.handleKey(control.About)
	if s.about.Visible || s.model.Quit || s.model.Notice != "Existing notice" {
		t.Fatal("toggle did not return intact")
	}
	s.handleKey(control.About)
	s.handleKey(control.Back)
	if s.about.Visible || s.model.Quit {
		t.Fatal("Back must close About, not exit")
	}
}

func TestAboutCannotInterruptPlayback(t *testing.T) {
	for _, kind := range []string{"Movie", "Audio", "Photo"} {
		s := testSession(t)
		s.model.Current().Detail = &jellyfin.Item{Type: kind}
		s.controller.running = kind != "Photo"
		s.handleKey(control.About)
		if s.about.Visible {
			t.Fatalf("About opened over %s", kind)
		}
	}
	s := testSession(t)
	s.controller.running = false
	s.media.pending = true
	s.handleKey(control.About)
	if s.about.Visible {
		t.Fatal("About opened during media handoff")
	}
}

func TestUpdateCheckIsAsyncAndSurvivesAboutClose(t *testing.T) {
	s := testSession(t)
	s.controller.running = false
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.ctx = ctx
	entered := make(chan struct{}, 2)
	gate := make(chan struct{})
	s.config.CheckUpdate = func(ctx context.Context) (release.Status, error) {
		entered <- struct{}{}
		select {
		case <-gate:
			return release.Status{Latest: "v1.0.0", Available: true}, nil
		case <-ctx.Done():
			return release.Status{}, ctx.Err()
		}
	}
	s.handleKey(control.About)
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("check not started")
	}
	s.handleKey(control.Select)
	s.handleKey(control.Back)
	if s.about.Visible || !s.about.Checking {
		t.Fatal("close canceled the release check")
	}
	select {
	case <-entered:
		t.Fatal("duplicate check")
	default:
	}
	close(gate)
	select {
	case result := <-s.events:
		s.handleResult(result)
	case <-time.After(time.Second):
		t.Fatal("closed About lost the release result")
	}
	if s.about.Visible || s.about.Checking || !s.about.Release.Available {
		t.Fatal("release result did not update the hidden page")
	}
}
