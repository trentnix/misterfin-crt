package browser

import "mistervision/internal/media"

// Navigation retains one connection's browsing location between Run calls.
// The caller owns the value and must not reuse it concurrently. It contains no
// credentials and is not persisted across application launches.
type Navigation struct {
	model    *Model
	identity media.Identity
}

// rememberNavigation runs after the loop has stopped mutating the model.
func (s *browserSession) rememberNavigation() {
	if s.config.Navigation == nil || s.client == nil || s.connectionChange == nil {
		return
	}
	s.model.Quit = false
	s.model.ExitConfirm = false
	*s.config.Navigation = Navigation{model: s.model, identity: s.client.Identity()}
}

// restoreNavigation rejects stale account state and clears canceled page work.
func (s *browserSession) restoreNavigation() {
	saved := s.config.Navigation
	if saved == nil || saved.model == nil || saved.identity != s.client.Identity() {
		return
	}
	s.model = saved.model
	s.model.Generation++
	s.model.Quit = false
	for i := range s.model.Stack {
		s.model.Stack[i].fetching = false
		s.model.Stack[i].Loading = false
		s.model.Stack[i].Target = s.model.Stack[i].Start + s.model.Stack[i].Selected
	}
}
