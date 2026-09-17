package rendering

import "mistervision/internal/connection"

// SetupKind selects the shared setup layout and activity indicator.
type SetupKind = connection.SetupKind

const (
	SetupHidden     = connection.SetupHidden
	SetupConnecting = connection.SetupConnecting
	SetupApproval   = connection.SetupApproval
	SetupFailure    = connection.SetupFailure
)

// SetupPresentation is safe, resolved content supplied by the connector.
// Rendering owns layout, not sign-in policy or provider-specific instructions.
type SetupPresentation = connection.Presentation
