package browser

import (
	"context"
	"errors"
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

func TestAboutUpdateIsOnlyPlaceholder(t *testing.T) {
	s := testSession(t)
	s.controller.running = false
	s.about.Visible = true
	s.handleKey(control.Open)
	if !s.about.UpdateNoticeUntil.IsZero() {
		t.Fatal("update without release")
	}
	s.handleResult(updateResult{status: release.Status{Latest: "v1.0.0", Available: true}})
	s.handleKey(control.Open)
	if s.about.Status(time.Now()) != "Not implemented yet." || !s.about.Visible || s.controller.running {
		t.Fatal("missing placeholder or unexpected navigation")
	}
	s.handleResult(updateResult{err: errors.New("network failure")})
	if !s.about.Release.Available || s.about.Message != "Could not check for updates." {
		t.Fatal("failed retry discarded known release")
	}
	s.handleResult(updateResult{err: release.ErrUnavailable})
	if s.about.Release.Available || s.about.Message != "No public release available." {
		t.Fatal("404 falsely reports an available update")
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

func TestAboutUpdateNoticeSurvivesChecksForTwoSeconds(t *testing.T) {
	s := testSession(t)
	s.controller.running = false
	s.about.Visible = true
	s.about.Release = release.Status{Latest: "v1.0.0", Available: true}
	before := time.Now()
	s.handleKey(control.Open)
	deadline := s.about.UpdateNoticeUntil
	if deadline.Before(before.Add(2*time.Second)) || deadline.After(time.Now().Add(2*time.Second)) {
		t.Fatal("update notice must last two seconds")
	}
	// A retry and its result must not erase the notice. Use explicit frame times
	// to verify both sides of the deadline without sleeping in the test.
	s.about.Checking = true
	if got := s.about.Status(deadline.Add(-time.Second)); got != "Not implemented yet." {
		t.Fatal(got)
	}
	s.handleResult(updateResult{err: release.ErrUnavailable})
	if got := s.about.Status(deadline.Add(-time.Millisecond)); got != "Not implemented yet." {
		t.Fatal(got)
	}
	if got := s.about.Status(deadline); got != "No public release available." {
		t.Fatal(got)
	}
	s.handleKey(control.Back)
	if s.about.Visible || !s.about.UpdateNoticeUntil.IsZero() {
		t.Fatal("Back must dismiss the page and notice")
	}
}
