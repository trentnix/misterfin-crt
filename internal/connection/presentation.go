// Package connection defines authentication and setup data consumed by the browser.
package connection

// SetupKind selects layout and activity, not a provider-specific error category.
// The connector supplies the text and retry action for each situation.
type SetupKind uint8

const (
	SetupHidden     SetupKind = iota // Ordinary browsing is visible.
	SetupConnecting                  // A connection attempt is running.
	SetupApproval                    // A public approval code is awaiting authorization.
	SetupFailure                     // Connection needs attention before retrying.
	SetupServers                     // The user chooses from discovered servers.
	SetupProfiles                    // The user chooses a viewing identity.
	SetupPIN                         // The user enters a private profile PIN.
	SetupConfirm                     // An account decision needs explicit approval.
)

// BackDestination identifies the preceding setup screen. The browser preserves
// it across progress and failure messages until navigation chooses a new screen.
type BackDestination uint8

const (
	BackDefault    BackDestination = iota // Open the connection chooser, or exit if none exists.
	BackServers                           // Return to server selection.
	BackProfiles                          // Return to the viewer picker.
	BackConnection                        // Restore the previously connected browser.
)

// Presentation is a copied setup snapshot. Text and Path must be safe to show.
// Code is public approval data, never an authentication secret. PathLabel names
// the configuration file or sign-in folder. Empty Retry disables retry input.
type Presentation struct {
	Kind                 SetupKind
	Title, Message, Code string
	Path, PathLabel      string
	Retry                string
	// Servers is an immutable snapshot. Selected is owned by the browser loop.
	Servers  []Server
	Selected int
	// SignIn adds a final picker action for linking another account. Empty hides it.
	SignIn string
	// Profiles contains public viewer information. PINLength exposes only masking.
	Profiles          []Profile
	AddUser           bool // The profile picker can start another sign-in.
	Forget            bool // The selected saved user can be removed from this device.
	PINLength, PINKey int
	PINChecking       bool // Verification is pending. Only cancellation accepts input.
	// Back preserves the preceding screen. Zero inherits the current destination
	// on progress updates. A new server picker establishes its own destination.
	Back BackDestination
	// Recovered records damaged sign-in storage that was backed up.
	Recovered bool
}

// RetryLabel returns the connector's label for the open/retry action.
func (s Presentation) RetryLabel() string { return s.Retry }

// ChoiceCount includes servers and the optional sign-in action.
func (s Presentation) ChoiceCount() int {
	if s.SignIn != "" {
		return len(s.Servers) + 1
	}
	return len(s.Servers)
}
