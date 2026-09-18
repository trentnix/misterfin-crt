package connection

import (
	"context"
	"errors"

	"mistervision/internal/media"
	"mistervision/internal/remote"
)

// Connector authenticates the backend selected by application assembly. Connect
// honors cancellation. The browser serializes calls. Progress and picker
// presentations are public. Profile replies can carry a private PIN, which
// must never enter presentation state, diagnostics, or persistent storage.
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
	// Endpoint is public metadata for the selected server. Discovery connectors
	// supply it after successful persistence, including address recovery.
	// Configured connectors may leave it zero when no discovery menu is needed.
	Endpoint  Server
	Server    media.Server
	Remote    remote.Source
	Recovered bool
	// Profile identifies the viewer. Nil means this connection has no profile UX.
	Profile *Profile
	// Avatars supplies optional artwork, including automatic profile reconnection.
	Avatars ProfileAvatars
	// ProfileAction is the available viewer action. Zero hides the action.
	ProfileAction ProfileAction
	// ForgetLabel names optional local credential removal, such as "Forget user".
	ForgetLabel string
}

// ErrRescan asks a server picker to refresh within the current sign-in attempt.
// Providers advertising a SignIn action must handle it without losing that account.
var ErrRescan = errors.New("refresh server choices")

// ErrChooseProfile returns from server selection to its preceding profile picker.
var ErrChooseProfile = errors.New("choose viewing profile")

// ErrCanceled means the user declined a confirmation. Credentials are unchanged.
var ErrCanceled = errors.New("connection action canceled")

// ErrSignedOut means the active credentials were removed. Retained sessions must
// be invalidated even if this error is joined with a subsequent storage failure.
var ErrSignedOut = errors.New("connection signed out")

// Interaction connects a sign-in worker to shared setup UI. Callbacks run on the
// worker and must honor cancellation. The browser owns selection and rendering.
type Interaction struct {
	// SelectServer requests a fresh choice instead of a remembered server.
	// Explicit connection configuration still takes precedence.
	SelectServer bool
	// NewAccount requests a fresh sign-in without replacing saved credentials
	// until the new account and its selected server connect successfully.
	NewAccount bool
	// ProfileAction requests a viewer change while preserving the active session.
	ProfileAction ProfileAction
	// Reauthenticate discards a retained account after authentication rejection.
	Reauthenticate bool
	// Progress publishes public status and approval codes. Nil discards progress.
	Progress func(Presentation)
	// ChooseServer waits for a choice or cancellation. Discovery requires it.
	ChooseServer func(context.Context, []Server) (Server, error)
	// ChooseProfile waits for a viewer and optional PIN. Nil cannot unlock profiles.
	ChooseProfile func(context.Context, ProfilePrompt) (ProfileSelection, error)
	// Confirm waits for explicit approval before removing or changing an identity.
	// Nil refuses the action. Cancellation must never imply approval.
	Confirm func(context.Context, Confirmation) (bool, error)
}

// Confirmation describes an account decision without credentials or raw errors.
type Confirmation struct {
	Title, Message, Accept string
}

// Ask requires an explicit confirmation callback and checks cancellation.
func (i Interaction) Ask(ctx context.Context, p Confirmation) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if i.Confirm == nil {
		return false, nil
	}
	accepted, err := i.Confirm(ctx, p)
	if err == nil {
		err = ctx.Err()
	}
	return accepted && err == nil, err
}

// Show publishes public connection progress when a UI is attached.
func (i Interaction) Show(p Presentation) {
	if i.Progress != nil {
		i.Progress(p)
	}
}
