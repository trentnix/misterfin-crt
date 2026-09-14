package browser

import (
	"context"

	"misterfin-crt/internal/jellyfin"
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
	s.status = "Connecting to Jellyfin..."
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
	jf := s.client
	go func() {
		var p jellyfin.Page
		var err error
		if req.Location.Kind == "views" {
			p, err = jf.Libraries(work)
		} else {
			p, err = jf.List(work, req.Location, req.Start, PageSize)
		}
		s.send(work, pageResult{request: *req, page: p, err: err})
	}()
}

func (s *browserSession) handleAuthCode(r authCodeResult) bool {
	if !s.connection.current(r.generation) {
		return false
	}
	s.status = "Quick Connect: " + r.code + "\nApprove this code in your Jellyfin client. Waiting for sign-in..."
	return true
}

func (s *browserSession) handleAuth(r authResult) bool {
	if !s.connection.current(r.generation) {
		return false
	}
	if r.err != nil {
		s.status = r.err.Error()
	} else {
		s.client = r.connection.client
		s.model = New()
		s.model.Rows = visibleRows(s.geometry.Width, s.geometry.Height)
		s.selection.key = ""
		s.selection.loader = r.connection.selection
		s.status = ""
		s.load(s.model.Load(0))
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
