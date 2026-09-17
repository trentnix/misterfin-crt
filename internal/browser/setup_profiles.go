package browser

import (
	"mistervision/internal/connection"
	"mistervision/internal/input/control"
)

// profileChoicesResult transfers public profiles and a one-use private reply
// channel. Stale requests cannot replace a newer setup screen or consume input.
type profileChoicesResult struct {
	generation int
	prompt     connection.ProfilePrompt
	choice     chan connection.ProfileSelection
}

func (r profileChoicesResult) apply(s *browserSession) bool {
	if !s.connection.current(r.generation) {
		return false
	}
	profiles := append([]connection.Profile(nil), r.prompt.Profiles...)
	for i := range profiles {
		if avatar := s.connection.profileAvatars[profiles[i].ID]; avatar != nil {
			profiles[i].Avatar = avatar
		}
	}
	s.connection.profileChoice = r.choice
	s.connection.profilePIN = ""
	s.connection.profileFlow = true
	s.setup = connection.Presentation{Kind: connection.SetupProfiles, Title: "Who’s watching?", Message: r.prompt.Message, Profiles: profiles, Selected: max(0, min(r.prompt.Selected, len(r.prompt.Profiles)-1))}
	if r.prompt.PIN {
		s.setup.Kind = connection.SetupPIN
	}
	return true
}

// handleProfileKey uses the existing direction and selection bindings for both
// avatars and a numeric keypad. Only the private reply carries entered digits.
func (s *browserSession) handleProfileKey(key control.Action) bool {
	p := &s.setup
	if key == control.Back {
		s.connection.profilePIN = ""
		p.PINLength = 0
		if p.Kind == connection.SetupPIN && p.PINChecking {
			s.connection.selectProfile = true
			s.authenticate()
			return true
		}
		if p.Kind == connection.SetupPIN {
			p.Kind = connection.SetupProfiles
			p.Message = ""
			return true
		}
		if s.config.ReturnConnectionID != "" {
			s.changeConnection(s.config.ReturnConnectionID)
			return true
		}
		return s.handleSetupBack()
	}
	if s.connection.profileChoice == nil || len(p.Profiles) == 0 {
		return false
	}
	if p.Kind == connection.SetupProfiles {
		switch key {
		case control.Previous:
			p.Selected = max(0, p.Selected-1)
		case control.Next:
			p.Selected = min(len(p.Profiles)-1, p.Selected+1)
		case control.Open:
			p.Message = ""
			if p.Profiles[p.Selected].Protected {
				p.Kind = connection.SetupPIN
				p.PINKey = 0
			} else {
				s.submitProfile()
			}
		}
		return true
	}
	switch key {
	case control.Previous:
		p.PINKey = max(p.PINKey/3*3, p.PINKey-1)
	case control.Next:
		p.PINKey = min(p.PINKey/3*3+2, p.PINKey+1)
	case control.Up:
		if p.PINKey >= 3 {
			p.PINKey -= 3
		}
	case control.Down:
		if p.PINKey < 9 {
			p.PINKey += 3
		}
	case control.Open:
		switch p.PINKey {
		case 9:
			pin := s.connection.profilePIN
			if len(pin) > 0 {
				s.connection.profilePIN = pin[:len(pin)-1]
			}
		case 11:
			return s.handleProfileKey(control.Back)
		default:
			digit := byte('1' + p.PINKey)
			if p.PINKey == 10 {
				digit = '0'
			}
			// A new digit acknowledges the previous rejection. Navigation and
			// Delete leave the feedback visible until another attempt starts.
			p.Message = ""
			s.connection.profilePIN += string(digit)
			if len(s.connection.profilePIN) == 4 {
				s.submitProfile()
				return true
			}
		}
		p.PINLength = len(s.connection.profilePIN)
	}
	return true
}

// submitProfile clears PIN input immediately and disables duplicate submissions.
func (s *browserSession) submitProfile() {
	s.connection.profileChoice <- connection.ProfileSelection{ID: s.setup.Profiles[s.setup.Selected].ID, PIN: s.connection.profilePIN}
	s.connection.profileChoice = nil
	s.connection.profilePIN = ""
	if s.setup.Kind == connection.SetupPIN {
		s.setup.PINLength = 4
		s.setup.PINChecking = true
		s.setup.Message = "Checking PIN..."
		return
	}
	s.setup = connection.Presentation{Kind: connection.SetupConnecting, Title: "Opening profile", Message: "Checking access to your media."}
}
