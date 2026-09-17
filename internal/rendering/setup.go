package rendering

import "mistervision/internal/connection"

// SetupKind selects the shared setup layout and activity indicator.
type SetupKind = connection.SetupKind

const (
	SetupHidden     = connection.SetupHidden
	SetupConnecting = connection.SetupConnecting
	SetupApproval   = connection.SetupApproval
	SetupFailure    = connection.SetupFailure
	SetupServers    = connection.SetupServers
	SetupProfiles   = connection.SetupProfiles
	SetupPIN        = connection.SetupPIN
)

// SetupPresentation is safe, resolved content from the connection flow.
// Rendering owns layout, not sign-in policy or provider-specific instructions.
type SetupPresentation = connection.Presentation
