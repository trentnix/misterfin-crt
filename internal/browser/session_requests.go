package browser

import (
	"context"
	"errors"
	"log/slog"
	"net/url"

	"mistervision/internal/media"
	"mistervision/internal/rendering"
)

// requestState owns the current listing request.
type requestState struct {
	cancel context.CancelFunc
}

// send delivers worker results unless that request has been canceled.
func (s *browserSession) send(work context.Context, r workerResult) {
	select {
	case s.events <- r:
	case <-work.Done():
	}
}

// authenticate resets browser state and delegates connection work. The
// connection manager rejects results from superseded attempts.
func (s *browserSession) authenticate() {
	s.stopRemote()
	if s.home.cancel != nil {
		s.home.cancel()
	}
	s.home = homeState{generation: s.home.generation + 1}
	s.requests.cancel()
	s.selection.cancel()
	s.selection.generation++
	s.selection.current = selectionData{}
	s.selection.err = ""
	s.selection.key = ""
	s.setup = s.setupPresentation(nil)
	s.connection.reauthenticate = s.client != nil
	s.connection.connect(s.ctx, s.send)
}

// load starts a listing request and cancels the previous listing request. Nil
// is a no-op. The model validates the generation when the result arrives.
// The caller must not mutate req after passing it here.
func (s *browserSession) load(req *Request) {
	if req == nil {
		return
	}
	if req.Location.Kind == "continue" {
		s.loadContinue()
		return
	}
	s.requests.cancel()
	work, stop := context.WithCancel(s.ctx)
	s.requests.cancel = stop
	client := s.client
	go func() {
		var p media.Page
		var err error
		if req.Location.Kind == "views" {
			p, err = client.Libraries(work)
		} else {
			p, err = client.List(work, req.Location, req.Start, PageSize)
		}
		s.send(work, pageResult{request: *req, page: p, err: err})
	}()
}

func (s *browserSession) handleAuthCode(r authCodeResult) bool {
	if !s.connection.current(r.generation) {
		return false
	}
	// The connector can restore discovery navigation for a remembered server.
	// Approval-code updates retain that action until the attempt is replaced.
	r.presentation.BackToServers = r.presentation.BackToServers || s.setup.BackToServers
	s.setup = r.presentation
	return true
}

func (s *browserSession) handleAuth(r authResult) bool {
	if !s.connection.current(r.generation) {
		return false
	}
	if r.err != nil {
		s.setup = s.setupPresentation(r.err)
	} else {
		s.connection.newAccount = false
		s.connection.profileFlow = false
		s.connection.selectProfile = false
		s.connection.profilePIN = ""
		s.about.Profile = r.connection.profile
		if s.about.Profile != nil {
			profile := *s.about.Profile
			if avatar := s.connection.profileAvatars[profile.ID]; avatar != nil {
				profile.Avatar = avatar
			}
			s.about.Profile = &profile
		}
		s.about.SwitchProfile = r.connection.switchProfile
		s.client = r.connection.client
		s.controlSource = r.connection.remote
		s.includeCurrentConnection()
		s.about.CurrentConnection = s.config.ConnectionID
		if r.connection.recovered {
			s.startupNotices = append(s.startupNotices, "Damaged sign-in was backed up. Connected successfully.")
		}
		s.model = New()
		s.restoreNavigation()
		s.model.Rows = rendering.VisibleRows(s.geometry.Width, s.geometry.Height)
		s.selection.key = ""
		s.selection.loader = r.connection.selection
		s.setup = rendering.SetupPresentation{}
		s.startRemote()
		if s.model.Current().Detail == nil {
			s.load(s.model.Load(s.model.Current().Start))
		} else {
			s.loadSelection()
		}
		s.refreshHome()
	}

	return true
}

func (s *browserSession) handlePage(r pageResult) bool {
	if r.request.Location.Kind == "views" && r.err == nil {
		r.page = s.homeLibraries(r.page)
	}
	if !s.model.Apply(r.request, r.page, r.err) {
		return false
	}
	// Record accepted pages after applying them, so request completion can be
	// distinguished from a screen that is ready for navigation. Escape and
	// bound the opaque parent ID. Never log library names or response bodies.
	parent := url.PathEscape(r.request.Location.ParentID)
	s.config.Diagnostics.Record("browser.page",
		slog.String("kind", r.request.Location.Kind),
		slog.String("parent", parent[:min(256, len(parent))]),
		slog.Int("start", r.request.Start), slog.Bool("failed", r.err != nil))
	if errors.Is(r.err, media.ErrUnauthorized) {
		s.selection.cancel()
		s.selection.generation++
		s.setup = s.setupPresentation(r.err)
	} else {
		s.loadSelection()
		s.load(s.model.Prefetch())
	}
	return true
}
