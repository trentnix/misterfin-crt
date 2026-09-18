package browser

import "mistervision/internal/connection"

// requireSignIn preserves an active account decision when background work loses
// authorization. The failure becomes visible if removal leaves the account open.
func (s *browserSession) requireSignIn(err error) {
	if s.connection.forgetting {
		s.pendingAuthError = err
		return
	}
	s.setup = s.setupPresentation(err)
}

// setupPresentation requests safe progress or recovery instructions. A missing
// connector is an assembly error, not a choice of default provider.
func (s *browserSession) setupPresentation(err error) connection.Presentation {
	if s.config.Connector != nil {
		p := s.config.Connector.Describe(err)
		p.Back = s.setup.Back
		return p
	}
	if err == nil {
		return connection.Presentation{Kind: connection.SetupConnecting, Title: "Connecting"}
	}
	return connection.Presentation{Kind: connection.SetupFailure, Title: titleServerUnavailable, Message: messageNoConnection, Retry: "Retry"}
}
