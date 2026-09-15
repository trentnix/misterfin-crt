package jellyfin

import "errors"

// Authentication failures identify recoverable sign-in conditions. Presentation
// and physical button instructions belong to the client UI, not this package.
var (
	// ErrQuickConnectDisabled means the server disallows code-based sign-in.
	ErrQuickConnectDisabled = errors.New("Quick Connect is disabled")
	// ErrQuickConnectExpired means the approval deadline elapsed.
	ErrQuickConnectExpired = errors.New("Quick Connect expired")
	// ErrUsernameNotFound means no server account matched the configured name.
	ErrUsernameNotFound = errors.New("configured username was not found")
	// ErrSessionSave means sign-in succeeded but persistent storage failed.
	ErrSessionSave = errors.New("signed in but cannot save session")
)
