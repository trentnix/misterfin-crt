package browser

import (
	"mistervision/internal/connection"
	"mistervision/internal/input/control"
)

// serverChoicesResult gives the event loop a one-use reply channel. Generation
// checks keep a canceled attempt from replacing a newer picker or selection.
type serverChoicesResult struct {
	generation int
	servers    []connection.Server
	choice     chan connection.Server
}

func (r serverChoicesResult) apply(s *browserSession) bool {
	if !s.connection.current(r.generation) {
		return false
	}
	s.connection.choice = r.choice
	s.setup = connection.Presentation{Kind: connection.SetupServers, Title: "Choose a server", Servers: r.servers}
	return true
}

// handleSetupKey keeps navigation on the event loop while connection work waits.
func (s *browserSession) handleSetupKey(key control.Action) bool {
	if key == control.Back {
		if s.setup.BackToServers {
			s.setup.BackToServers = false
			s.connection.selectServer = true
			s.authenticate()
			return true
		}
		if len(s.about.Connections) > 0 {
			s.about.Visible = true
			s.about.ConnectionsVisible = true
			s.about.ConnectionPath = nil
			s.about.ConnectionSelected = 0
			s.about.ConnectionMessage = ""
			return true
		}
		s.connection.cancel()
		s.model.Quit = true
		return false
	}
	if s.setup.Kind == connection.SetupServers {
		switch key {
		case control.Up:
			s.setup.Selected = max(0, s.setup.Selected-1)
		case control.Down:
			s.setup.Selected = max(0, min(len(s.setup.Servers)-1, s.setup.Selected+1))
		case control.Open:
			if s.connection.choice != nil && len(s.setup.Servers) > 0 {
				s.connection.choice <- s.setup.Servers[s.setup.Selected]
				s.connection.choice = nil
				s.connection.selectServer = false
				s.setup = s.setupPresentation(nil)
				s.setup.BackToServers = true
			}
		case control.Retry, control.Select:
			s.connection.selectServer = true
			s.authenticate()
		}
	} else if (key == control.Retry || key == control.Open) && s.setup.RetryLabel() != "" {
		s.authenticate()
	}
	return true
}
