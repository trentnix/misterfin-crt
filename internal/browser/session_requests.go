package browser

import (
	"context"
	"errors"
	"log/slog"
	"net/url"

	"mistervision/internal/connection"
	"mistervision/internal/diagnostics"
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
	s.about.AccountMessage = ""
	s.pendingAuthError = nil
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
	if s.connection.profileAction == connection.ProfileForget {
		s.setup = connection.Presentation{Kind: connection.SetupConnecting, Title: titleSignOutCleanup, Message: messageSignOutCleanup}
	}
	s.connection.reauthenticate = s.client != nil
	s.connection.connect(s.ctx, s.send)
}

// load starts a listing request and cancels the previous listing request. Nil
// is a no-op. Results must match both authentication and navigation generations.
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
	client, generation := s.client, s.connection.generation
	go func() {
		var p media.Page
		var err error
		if req.Location.Kind == "views" {
			p, err = client.Libraries(work)
		} else {
			p, err = client.List(work, req.Location, req.Start, PageSize)
		}
		s.send(work, pageResult{connectionGeneration: generation, request: *req, page: p, err: err})
	}()
}

func (s *browserSession) handleAuthCode(r authCodeResult) bool {
	if !s.connection.current(r.generation) {
		return false
	}
	// Progress retains the preceding screen. A new server picker starts its
	// own navigation route, even when it has no preceding setup screen.
	if r.presentation.Back == connection.BackDefault && r.presentation.Kind != connection.SetupServers {
		r.presentation.Back = s.setup.Back
	}
	s.setup = r.presentation
	return true
}

func (s *browserSession) handleAuth(r authResult) bool {
	if !s.connection.current(r.generation) {
		return false
	}
	if errors.Is(r.err, connection.ErrSignedOut) {
		s.stopRemote()
		// Discard background results from the removed account, including errors
		// that could otherwise replace cleanup recovery instructions.
		s.pendingAuthError = nil
		s.requests.cancel()
		s.model.Generation++
		if s.home.cancel != nil {
			s.home.cancel()
		}
		s.home.generation++
		s.selection.cancel()
		s.selection.generation++
		s.client, s.controlSource = nil, nil
		s.about.Profile, s.about.ForgetLabel = nil, ""
		s.about.ProfileAction = connection.ProfileUnchanged
		s.config.ReturnConnectionID = ""
		s.about.CanReturnToConnection = false
		if s.config.Navigation != nil {
			*s.config.Navigation = Navigation{}
		}
		if r.err == connection.ErrSignedOut {
			s.connectionChange = &connection.Change{ID: s.config.ConnectionID}
			s.model.Quit = true
		} else {
			// Removal has committed. Retry must finish cleanup before sign-in.
			s.connection.profileAction = connection.ProfileForget
			s.setup = s.config.Connector.Describe(r.err)
		}
		return true
	}
	if errors.Is(r.err, connection.ErrCanceled) && s.config.ReturnConnectionID != "" {
		s.changeConnection(s.config.ReturnConnectionID)
		return true
	}
	if r.err != nil {
		s.setup = s.setupPresentation(r.err)
	} else {
		s.connection.newAccount = false
		s.connection.profileAction = connection.ProfileUnchanged
		s.connection.profilePIN = ""
		s.about.Profile = r.connection.profile
		if s.about.Profile != nil {
			profile := *s.about.Profile
			if avatar := s.connection.profileAvatars[profileAvatarKey(profile.ID, profile.AvatarKey)]; avatar != nil {
				profile.Avatar = avatar
			}
			s.about.Profile = &profile
		}
		s.about.ProfileAction = r.connection.profileAction
		s.about.ForgetLabel = r.connection.forgetLabel
		s.client = r.connection.client
		s.controlSource = r.connection.remote
		s.refreshConnections()
		s.about.CurrentConnection = s.config.ConnectionID
		if r.connection.recovered {
			s.startupNotices = append(s.startupNotices, messageRecoveredSignIn)
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
	if !s.connection.current(r.connectionGeneration) {
		return false
	}
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
		slog.Int("start", r.request.Start), slog.Bool("failed", r.err != nil), slog.String("error_kind", diagnostics.ErrorKind(r.err)))
	if errors.Is(r.err, media.ErrUnauthorized) {
		s.selection.cancel()
		s.selection.generation++
		s.requireSignIn(r.err)
	} else {
		s.loadSelection()
		s.load(s.model.Prefetch())
	}
	return true
}
