package browser

import (
	"context"
	"errors"
	"time"

	"mistervision/internal/input/control"
	"mistervision/internal/release"
	"mistervision/internal/rendering"
	"mistervision/internal/update"
)

// checkUpdate starts at most one request at a time. The application starts one
// check per run. Explicit retries are allowed after completion. Closing About
// keeps the check alive so the carousel can show an availability notice.
func (s *browserSession) checkUpdate() {
	if s.about.Checking || s.about.NotesVisible || s.about.Updating || s.config.CheckUpdate == nil {
		return
	}
	s.about.Checking = true
	s.about.Message = ""
	check := s.config.CheckUpdate
	go func() {
		work, cancel := context.WithTimeout(s.ctx, 10*time.Second)
		defer cancel()
		status, err := check(work)
		s.send(s.ctx, updateResult{status: status, err: err})
	}()
}

type updateResult struct {
	status release.Status
	err    error
}

// apply updates release state on the browser loop. Raw network errors never
// reach the screen. A failed retry retains any previously verified release.
func (r updateResult) apply(s *browserSession) bool {
	s.about.Checking = false
	s.about.Checked = true
	switch {
	case errors.Is(r.err, release.ErrUnavailable):
		s.about.Release = release.Status{}
		s.about.Message = "No public release available."
	case r.err != nil:
		s.about.Message = "Could not check for updates."
	default:
		s.about.Release = r.status
		s.about.Notes = rendering.ReleaseNotes(r.status.Notes, max(320, s.geometry.Width))
		s.about.Scroll = 0
		s.about.Message = ""
	}
	return true
}

// handleAboutKey isolates page controls from navigation and playback. Returning
// to the preceding screen preserves its selection, notices, and pending work.
func (s *browserSession) handleAboutKey(key control.Action) bool {
	if s.about.Updating {
		if key == control.Back && s.about.Progress.Phase != update.Installing && s.update.cancel != nil {
			s.update.cancel()
		}
		return true
	}
	if s.about.ConnectionsVisible {
		return s.handleConnectionKey(key)
	}
	if s.about.Installed || !s.update.exitAt.IsZero() {
		// Keep the completion message visible until its deadline.
		return false
	}
	switch key {
	case control.About, control.Back:
		if s.about.NotesVisible && key == control.Back {
			s.about.NotesVisible = false
			s.about.Message = ""
		} else {
			s.about.Visible = false
			s.about.NotesVisible = false
		}
	case control.Open:
		if s.about.Release.Available && !s.about.Checking {
			if s.about.NotesVisible {
				s.installUpdate()
			} else {
				s.about.NotesVisible = true
				s.about.Message = ""
			}
		}
	case control.Up:
		if s.about.NotesVisible {
			s.about.Scroll = max(0, s.about.Scroll-1)
		}
	case control.Down:
		if !s.about.NotesVisible && len(s.about.Connections) > 0 {
			s.about.ConnectionsVisible = true
			s.about.ConnectionMessage = ""
		}
		if s.about.NotesVisible {
			s.about.Scroll = min(s.about.ScrollLimit(max(320, s.geometry.Width), max(240, s.geometry.Height), s.controls), s.about.Scroll+1)
		}
	case control.Select, control.Retry:
		s.checkUpdate()
	}
	return true
}
