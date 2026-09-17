package connection

import (
	"context"
	"errors"

	"mistervision/internal/media"
	"mistervision/internal/remote"
)

// Connector authenticates the backend selected by application assembly. Connect
// reloads credentials on each attempt and honors cancellation. The browser
// serializes calls. Interaction carries public progress and server selection,
// never credentials.
// Describe(nil) presents initial progress. Describe(err) supplies safe recovery
// instructions for connection failures and later authentication rejection.
type Connector interface {
	Connect(context.Context, Interaction) (Session, error)
	Describe(error) Presentation
}

// Session supplies services scoped to an authenticated account. Remote is optional.
// The browser owns cancellation of Remote.Run. Recovered records damaged sign-in
// storage that was backed up before successful authentication.
type Session struct {
	Server    media.Server
	Remote    remote.Source
	Recovered bool
	// Profile identifies the viewer. Nil means this connection has no profile UX.
	Profile *Profile
	// Avatars supplies optional artwork, including automatic profile reconnection.
	Avatars ProfileAvatars
	// SwitchProfile enables the optional About action for this connection.
	SwitchProfile bool
}

// ErrRescan asks a server picker to refresh within the current sign-in attempt.
// Providers advertising a SignIn action must handle it without losing that account.
var ErrRescan = errors.New("refresh server choices")

// ErrChooseProfile returns from server selection to its preceding profile picker.
var ErrChooseProfile = errors.New("choose viewing profile")

// Interaction connects a sign-in worker to shared setup UI. Callbacks run on the
// worker and must honor cancellation. The browser owns selection and rendering.
type Interaction struct {
	// SelectServer requests a fresh choice instead of a remembered server.
	// Explicit connection configuration still takes precedence.
	SelectServer bool
	// NewAccount requests a fresh sign-in without replacing saved credentials
	// until the new account and its selected server connect successfully.
	NewAccount bool
	// SelectProfile requests a fresh viewer choice while preserving the account.
	SelectProfile bool
	// Reauthenticate discards a retained account after authentication rejection.
	Reauthenticate bool
	// Progress publishes public status and approval codes. Nil discards progress.
	Progress func(Presentation)
	// ChooseServer waits for a choice or cancellation. Discovery requires it.
	ChooseServer func(context.Context, []Server) (Server, error)
	// ChooseProfile waits for a viewer and optional PIN. Nil cannot unlock profiles.
	ChooseProfile func(context.Context, ProfilePrompt) (ProfileSelection, error)
}

// Show publishes public connection progress when a UI is attached.
func (i Interaction) Show(p Presentation) {
	if i.Progress != nil {
		i.Progress(p)
	}
}
