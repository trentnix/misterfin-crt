package browser

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"misterfin-crt/internal/input/control"
	"misterfin-crt/internal/platform"
	"misterfin-crt/internal/playback"
	"misterfin-crt/internal/release"
	"misterfin-crt/internal/remote"
	"misterfin-crt/internal/rendering"
	"misterfin-crt/internal/update"
	"misterfin-crt/internal/videoout"
)

type testUpdater func(context.Context, release.Status, func(update.Progress)) error

func (f testUpdater) Install(ctx context.Context, s release.Status, notify func(update.Progress)) error {
	return f(ctx, s, notify)
}

func updateSession(t *testing.T) *browserSession {
	s := testSession(t)
	s.controller.running = false
	s.geometry = platform.Geometry{Width: 640, Height: 240}
	s.about.Visible = true
	s.handleResult(updateResult{status: release.Status{Latest: "v0.2.0", Available: true, HasBundle: true, Notes: strings.Repeat("A release-note line.\n", 40)}})
	return s
}

func nextUpdateResult(t *testing.T, s *browserSession) {
	t.Helper()
	select {
	case result := <-s.events:
		s.handleResult(result)
	case <-time.After(3 * time.Second):
		t.Fatal("missing update result")
	}
}

func TestUpdateRequiresNotesAndSeparateConfirmation(t *testing.T) {
	s := updateSession(t)
	entered := make(chan struct{})
	s.config.Updater = testUpdater(func(ctx context.Context, status release.Status, notify func(update.Progress)) error {
		if status.Latest != "v0.2.0" {
			t.Error("wrong release")
		}
		close(entered)
		<-ctx.Done()
		return ctx.Err()
	})
	t.Cleanup(s.update.close)
	s.handleKey(control.Open)
	if !s.about.NotesVisible || s.about.Updating {
		t.Fatal("first Open must only show notes")
	}
	select {
	case <-entered:
		t.Fatal("installed without confirmation")
	default:
	}
	s.handleKey(control.Open)
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("installer did not start")
	}
	if !s.about.Updating {
		t.Fatal("missing progress state")
	}
	if s.handleRemote(remote.Command{Kind: remote.Play}) {
		t.Fatal("remote playback accepted during installation")
	}
	s.handleKey(control.Back)
	nextUpdateResult(t, s)
	if s.about.Updating || !strings.Contains(s.about.Message, "canceled") || !s.about.NotesVisible {
		t.Fatal("cancel did not return to notes")
	}
	s.handleKey(control.Back)
	if s.about.NotesVisible || !s.about.Visible {
		t.Fatal("Back did not return to About")
	}
}

func TestUpdateOutcomesAndErrorsStayInAbout(t *testing.T) {
	for _, kind := range []string{"success", "failure", "manual", "recovery"} {
		t.Run(kind, func(t *testing.T) {
			s := updateSession(t)
			s.model.Current().Selected = 3
			s.about.NotesVisible = true
			var result error
			switch kind {
			case "failure":
				result = errors.New("private server path")
			case "manual":
				result = update.ErrManual
			case "recovery":
				result = update.ErrRecovery
			}
			s.config.Updater = testUpdater(func(context.Context, release.Status, func(update.Progress)) error { return result })
			s.handleKey(control.Open)
			t.Cleanup(s.update.close)
			nextUpdateResult(t, s)
			if s.about.Updating || s.model.Current().Selected != 3 || strings.Contains(s.about.Message, "private") {
				t.Fatal("update changed selection or exposed raw errors")
			}
			if kind == "success" && !s.about.Installed {
				t.Fatal("success not shown")
			}
			if kind == "success" || kind == "recovery" {
				if s.update.exitAt.IsZero() || s.update.exitAt.Before(time.Now()) {
					t.Fatal("missing exit deadline")
				}
				if s.handleRemote(remote.Command{Kind: remote.Play}) {
					t.Fatal("playback before restart")
				}
			} else if !s.update.exitAt.IsZero() {
				t.Fatal("ordinary failure exited application")
			}
		})
	}
}

func TestManualTargetAndPlaybackDoNotInstall(t *testing.T) {
	s := updateSession(t)
	s.handleKey(control.Open)
	s.handleKey(control.Open)
	if s.about.Updating || !strings.Contains(s.about.Status(), "manually") {
		t.Fatal("manual target offered automatic installation")
	}
	s.config.Updater = testUpdater(func(context.Context, release.Status, func(update.Progress)) error {
		t.Fatal("installed during playback")
		return nil
	})
	s.controller.running = true
	s.installUpdate()
	if s.about.Message != "Stop playback before installing an update." {
		t.Fatal(s.about.Message)
	}
}

func TestUpdateShutdownWaitsForRollback(t *testing.T) {
	s := updateSession(t)
	entered, rollback, finish := make(chan struct{}), make(chan struct{}), make(chan struct{})
	s.config.Updater = testUpdater(func(ctx context.Context, _ release.Status, _ func(update.Progress)) error {
		close(entered)
		<-ctx.Done()
		close(rollback)
		<-finish
		return ctx.Err()
	})
	s.about.NotesVisible = true
	s.installUpdate()
	<-entered
	closed := make(chan struct{})
	go func() { s.update.close(); close(closed) }()
	<-rollback
	select {
	case <-closed:
		t.Fatal("shutdown returned before rollback")
	default:
	}
	close(finish)
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not finish")
	}
}

func TestNotesScrollingStopsAtLastVisiblePage(t *testing.T) {
	s := updateSession(t)
	s.handleKey(control.Open)
	for range 100 {
		s.handleKey(control.Down)
	}
	limit := s.about.ScrollLimit(640, 240, s.controls)
	if s.about.Scroll != limit {
		t.Fatalf("scroll %d, limit %d", s.about.Scroll, limit)
	}
	s.handleKey(control.Up)
	if s.about.Scroll != limit-1 {
		t.Fatal("one Up did not move from bottom")
	}
	for range 100 {
		s.handleKey(control.Up)
	}
	if s.about.Scroll != 0 {
		t.Fatal("scrolled above notes")
	}
}

func TestFailedCheckPreservesKnownRelease(t *testing.T) {
	s := updateSession(t)
	s.handleResult(updateResult{err: errors.New("network failure")})
	if !s.about.Release.Available || s.about.Message != "Could not check for updates." {
		t.Fatal("lost known release")
	}
	s.handleResult(updateResult{err: release.ErrUnavailable})
	if s.about.Release.Available || s.about.Message != "No public release available." {
		t.Fatal("missing release remained available")
	}
}

// TestUpdateExitThroughBrowser exercises the installation result, completion
// screen, exit deadline, and resource cleanup through the real browser loop.
func TestUpdateExitThroughBrowser(t *testing.T) {
	for _, tc := range []struct {
		name        string
		automatic   bool
		installErr  error
		wantRestart bool
	}{
		{"restart", true, nil, true},
		{"older launcher", false, nil, false},
		{"failure", true, errors.New("download failed"), false},
		{"canceled", true, context.Canceled, false},
		{"recovery", true, update.ErrRecovery, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
			defer cancel()
			keys := make(chan control.Event, 8)
			dir := t.TempDir()
			finished := false
			installedAt := time.Time{}
			renderer := &updateTestRenderer{Renderer: rendering.NewRenderer()}
			started := false
			renderer.inspect = func(scene rendering.Scene) {
				if scene.About.Checked && !started {
					started = true
					for _, key := range []control.Action{control.About, control.Open, control.Open} {
						keys <- control.Event{Action: key}
					}
				}
				if scene.About.Installed && installedAt.IsZero() {
					installedAt = time.Now()
					if scene.About.Restarting != tc.automatic {
						t.Error("completion message does not match launcher capability")
					}
					// Ordinary navigation must not erase the success message.
					keys <- control.Event{Action: control.Back}
				}
				if tc.installErr != nil && !errors.Is(tc.installErr, update.ErrRecovery) && scene.About.Message != "" {
					keys <- control.Event{Action: control.Quit}
				}
			}
			config := Config{
				StateDir:           dir,
				RestartAfterUpdate: tc.automatic,
				CheckUpdate: func(context.Context) (release.Status, error) {
					return release.Status{Available: true, HasBundle: true, Latest: "v1.1.0"}, nil
				},
				Updater: testUpdater(func(context.Context, release.Status, func(update.Progress)) error {
					finished = true
					return tc.installErr
				}),
			}
			output := &runTestOutput{}
			err := Run(ctx, config, playback.Config{}, output, renderer, nil, keys)
			if update.RestartRequested(err) != tc.wantRestart || (err != nil && !tc.wantRestart) {
				t.Fatalf("unexpected browser result: %v", err)
			}
			if !finished || !output.cleared || output.closed || ctx.Err() != nil {
				t.Fatalf("incorrect shutdown: installed=%v cleared=%v closed=%v context=%v", finished, output.cleared, output.closed, ctx.Err())
			}
			if tc.installErr == nil && (installedAt.IsZero() || time.Since(installedAt) < 1900*time.Millisecond) {
				t.Fatal("completion screen dismissed before restart deadline")
			}
		})
	}
}

type updateTestRenderer struct {
	rendering.Renderer
	inspect func(rendering.Scene)
}

func (r *updateTestRenderer) Render(width, height int, scene rendering.Scene) videoout.Frame {
	r.inspect(scene)
	return r.Renderer.Render(width, height, scene)
}
