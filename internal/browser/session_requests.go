package browser

import (
	"context"

	"misterfin-crt/internal/jellyfin"
)

// requestState owns the current authentication or listing request.
type requestState struct {
	cancel         context.CancelFunc
	authGeneration int
}

// send delivers worker results unless that request has been canceled.
func (s *browserSession) send(work context.Context, r result) {
	select {
	case s.events <- r:
	case <-work.Done():
	}
}

// authenticate replaces pending authentication or page work and invalidates
// selection work. A generation check prevents earlier sign-in attempts from changing
// the current screen, even if a canceled worker still delivers its result.
func (s *browserSession) authenticate() {
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
	s.requests.authGeneration++
	generation := s.requests.authGeneration
	work, stop := context.WithCancel(s.ctx)
	s.requests.cancel = stop
	s.status = "Connecting to Jellyfin..."
	go func() {
		c, err := jellyfin.LoadConfig(s.configPath)
		var jf *jellyfin.Client
		if err == nil {
			var session jellyfin.Session
			session, err = jellyfin.LoadSession(s.stateDir, c.Server)
			if err == nil {
				jf = jellyfin.NewClient(c, session)
				err = jf.Authenticate(work, s.stateDir, func(code string) {
					s.send(work, result{kind: authResult, request: Request{Generation: generation}, code: code})
				})
			}
		}
		s.send(work, result{kind: authResult, request: Request{Generation: generation}, client: jf, err: err})
	}()
}

// load starts a listing request and cancels the previous authentication or
// listing request. Nil is a no-op. The model validates the request generation
// when the result arrives. The caller must not mutate req after passing it here.
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
	jf := s.client
	go func() {
		var p jellyfin.Page
		var err error
		if req.Location.Kind == "views" {
			p, err = jf.Libraries(work)
		} else {
			p, err = jf.List(work, req.Location, req.Start, PageSize)
		}
		s.send(work, result{request: *req, page: p, err: err})
	}()
}

func (s *browserSession) handleAuth(r result) bool {
	if r.request.Generation != s.requests.authGeneration {
		return false
	}
	if r.code != "" {
		s.status = "Quick Connect: " + r.code + "\nApprove this code in your Jellyfin client. Waiting for sign-in..."
	} else if r.err != nil {
		s.status = r.err.Error()
	} else {
		s.client = r.client
		s.model = New()
		s.model.Rows = visibleRows(s.geometry.Width, s.geometry.Height)
		s.selection.key = ""
		s.selection.loader = newSelectionLoader(s.client, s.geometry.Width, s.geometry.Height)
		s.selection.loader.disk = newMosaicDiskCache(mosaicCacheRoot(s.driver.options.Headless), s.client.Config.Server, s.client.Session.UserID)
		s.selection.loader.artwork.disk = newArtworkDiskCache(browserCacheRoot(s.driver.options.Headless, "covercache"), s.client.Config.Server, s.client.Session.UserID)
		s.status = ""
		s.load(s.model.Load(0))
		s.refreshHome()
	}

	return true
}

func (s *browserSession) handlePage(r result) bool {
	if r.request.Location.Kind == "views" && r.err == nil {
		r.page = s.homeLibraries(r.page)
	}
	if !s.model.Apply(r.request, r.page, r.err) {
		return false
	}
	if jellyfin.Rejected(r.err) {
		s.selection.cancel()
		s.selection.generation++
		s.status = "Session rejected. Press R to sign in again."
	} else {
		s.loadSelection()
		s.load(s.model.Prefetch())
	}
	return true
}
