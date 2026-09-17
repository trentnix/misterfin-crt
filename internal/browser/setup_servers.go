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
	choice     chan serverChoice
}

func (r serverChoicesResult) apply(s *browserSession) bool {
	if !s.connection.current(r.generation) {
		return false
	}
	s.connection.choice = r.choice
	if s.setup.Kind != connection.SetupServers {
		s.setup = connection.Presentation{Kind: connection.SetupServers, Title: "Choose a server"}
	}
	s.setup.Servers, s.setup.Selected, s.setup.BackToServers = r.servers, 0, false
	return true
}

// handleSetupKey keeps navigation on the event loop while connection work waits.
func (s *browserSession) handleSetupKey(key control.Action) bool {
	if s.setup.Kind == connection.SetupProfiles || s.setup.Kind == connection.SetupPIN {
		return s.handleProfileKey(key)
	}
	if key == control.Back {
		return s.handleSetupBack()
	}
	if s.setup.Kind == connection.SetupServers {
		switch key {
		case control.Up:
			s.setup.Selected = max(0, s.setup.Selected-1)
		case control.Down:
			s.setup.Selected = max(0, min(s.setup.ChoiceCount()-1, s.setup.Selected+1))
		case control.Open:
			if s.connection.choice != nil && s.setup.SignIn != "" && s.setup.Selected == len(s.setup.Servers) {
				s.connection.newAccount = true
				s.connection.selectServer = true
				s.setup.BackToServers = true
				s.authenticate()
			} else if s.connection.choice != nil && s.setup.Selected >= 0 && s.setup.Selected < len(s.setup.Servers) {
				s.connection.choice <- serverChoice{server: s.setup.Servers[s.setup.Selected]}
				s.connection.choice = nil
				s.connection.selectServer = false
				s.setup = s.setupPresentation(nil)
				s.setup.BackToServers = true
			}
		case control.Retry, control.Select:
			if s.setup.SignIn != "" {
				if s.connection.choice != nil {
					s.connection.choice <- serverChoice{err: connection.ErrRescan}
					s.connection.choice = nil
				}
			} else {
				s.connection.selectServer = true
				s.authenticate()
			}
		}
	} else if (key == control.Retry || key == control.Open) && s.setup.RetryLabel() != "" {
		s.authenticate()
	}
	return true
}

// handleSetupBack opens the preceding setup route without changing a pending
// prompt. Dismissing Connections can therefore restore the screen beneath it.
func (s *browserSession) handleSetupBack() bool {
	if s.setup.Kind == connection.SetupServers && s.setup.BackToProfiles && s.connection.choice != nil {
		s.connection.choice <- serverChoice{err: connection.ErrChooseProfile}
		s.connection.choice = nil
		s.setup = connection.Presentation{Kind: connection.SetupConnecting, Title: "Opening profiles"}
		return true
	}
	if s.connection.profileFlow && s.config.ReturnConnectionID != "" && s.setup.Kind != connection.SetupServers {
		s.changeConnection(s.config.ReturnConnectionID)
		return true
	}
	s.connection.newAccount = false
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
