package browser

import (
	"mistervision/internal/connection"
	"mistervision/internal/input/control"
	"mistervision/internal/rendering"
)

// handleConnectionKey keeps the active session intact while the user browses
// choices. Only a usable selection requests a new session from the application.
func (s *browserSession) handleConnectionKey(key control.Action) bool {
	a := &s.about
	_, choices := a.ConnectionChoices()
	switch key {
	case control.Back:
		if a.ConnectionMessage != "" {
			a.ConnectionMessage = ""
			return true
		}
		if len(a.ConnectionPath) > 0 {
			last := len(a.ConnectionPath) - 1
			a.ConnectionSelected = a.ConnectionPath[last]
			a.ConnectionPath = a.ConnectionPath[:last]
		} else if s.setup.Kind != rendering.SetupHidden {
			// Setup has no carousel beneath it. Leave the attempt instead of
			// cycling through About and the server picker again.
			if s.config.ReturnConnectionID != "" {
				s.changeConnection(s.config.ReturnConnectionID)
			} else {
				s.connection.cancel()
				s.model.Quit = true
			}
		} else {
			a.ConnectionsVisible = false
		}
		a.ConnectionMessage = ""
	case control.About:
		a.ConnectionsVisible = false
		a.Visible = false
		a.ConnectionPath = nil
		a.ConnectionSelected = 0
	case control.Up:
		a.ConnectionSelected = max(0, a.ConnectionSelected-1)
		a.ConnectionMessage = ""
	case control.Down:
		a.ConnectionSelected = min(max(0, len(choices)-1), a.ConnectionSelected+1)
		a.ConnectionMessage = ""
	case control.Open:
		if a.ConnectionSelected < 0 || a.ConnectionSelected >= len(choices) {
			return false
		}
		choice := choices[a.ConnectionSelected]
		if len(choice.Children) > 0 {
			a.ConnectionPath = append(a.ConnectionPath, a.ConnectionSelected)
			a.ConnectionSelected = 0
		} else if choice.Help != "" {
			a.ConnectionMessage = choice.Help
		} else {
			s.changeConnection(choice.ID)
		}
	}
	return true
}

// includeCurrentConnection adds a newly discovered account to the menu snapshot
// after authentication. It never mutates choices borrowed from configuration.
func (s *browserSession) includeCurrentConnection() {
	id := s.config.ConnectionID
	if id == "" || len(s.about.Connections) == 0 {
		return
	}
	choices := append([]connection.Choice(nil), s.about.Connections...)
	group := -1
	for i, c := range choices {
		if c.ID == "existing" {
			group = i
			for _, child := range c.Children {
				if child.ID == id {
					return
				}
			}
		}
	}
	current := connection.Choice{ID: id, Name: "Current connection", Description: s.client.Identity().Server}
	if group < 0 {
		choices = append([]connection.Choice{{ID: "existing", Name: "Use existing connection", Description: "Choose a configured or remembered server", Children: []connection.Choice{current}}}, choices...)
	} else {
		choices[group].Children = append(append([]connection.Choice(nil), choices[group].Children...), current)
	}
	s.about.Connections = choices
}

// changeConnection carries the last working route through unfinished attempts.
// Assembly reuses its retained sign-in and browsing state when setup is canceled.
func (s *browserSession) changeConnection(id string) {
	returnID := s.config.ReturnConnectionID
	if s.client != nil && s.setup.Kind == rendering.SetupHidden {
		if id == s.config.ConnectionID {
			s.about.Visible = false
			s.about.ConnectionsVisible = false
			s.about.ConnectionPath = nil
			s.about.ConnectionSelected = 0
			return
		}
		returnID = s.config.ConnectionID
	}
	s.connectionChange = &connection.Change{ID: id, ReturnID: returnID}
	s.model.Quit = true
}
