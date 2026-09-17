package connection

import (
	"context"

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
}

// Interaction connects a sign-in worker to shared setup UI. Callbacks run on the
// worker and must honor cancellation. The browser owns selection and rendering.
type Interaction struct {
	// SelectServer requests a fresh choice instead of a remembered server.
	// Explicit connection configuration still takes precedence.
	SelectServer bool
	// Reauthenticate discards a retained account after authentication rejection.
	Reauthenticate bool
	// Progress publishes public status and approval codes. Nil discards progress.
	Progress func(Presentation)
	// ChooseServer waits for a choice or cancellation. Discovery requires it.
	ChooseServer func(context.Context, []Server) (Server, error)
}

// Show publishes public connection progress when a UI is attached.
func (i Interaction) Show(p Presentation) {
	if i.Progress != nil {
		i.Progress(p)
	}
}
