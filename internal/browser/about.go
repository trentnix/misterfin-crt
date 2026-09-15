package browser

import (
	"context"
	"errors"
	"time"

	"misterfin-crt/internal/input/control"
	"misterfin-crt/internal/release"
)

// checkUpdate starts at most one request at a time. The application starts one
// check per run. Explicit retries are allowed after completion. Closing About
// keeps the check alive so the carousel can show an availability notice.
func (s *browserSession) checkUpdate() {
	if s.about.Checking || s.config.CheckUpdate == nil {
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
		s.about.Message = ""
	}
	return true
}

// handleAboutKey isolates page controls from navigation and playback. Returning
// to the preceding screen preserves its selection, notices, and pending work.
func (s *browserSession) handleAboutKey(key control.Action) bool {
	switch key {
	case control.About, control.Back:
		s.about.Visible = false
		s.about.UpdateNoticeUntil = time.Time{}
	case control.Open:
		if s.about.Release.Available && !s.about.Checking {
			s.about.UpdateNoticeUntil = time.Now().Add(2 * time.Second)
		}
	case control.Select, control.Retry:
		s.checkUpdate()
	}
	return true
}
