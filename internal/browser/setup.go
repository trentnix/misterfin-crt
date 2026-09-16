package browser

import "misterfin-crt/internal/connection"

// setupPresentation requests safe progress or recovery instructions. A missing
// connector is an assembly error, not a choice of default provider.
func (s *browserSession) setupPresentation(err error) connection.Presentation {
	if s.config.Connector != nil {
		return s.config.Connector.Describe(err)
	}
	if err == nil {
		return connection.Presentation{Kind: connection.SetupConnecting, Title: "Connecting"}
	}
	return connection.Presentation{Kind: connection.SetupFailure, Title: "Server unavailable", Message: "No server connection is configured.", Retry: "Retry"}
}
