package connection

import (
	"context"

	"misterfin-crt/internal/media"
	"misterfin-crt/internal/remote"
)

// Connector authenticates the backend selected by application assembly. Connect
// reloads credentials on each attempt and honors cancellation. The browser
// serializes calls. Progress contains public approval data, never credentials.
// Describe(nil) presents initial progress. Describe(err) supplies safe recovery
// instructions for connection failures and later authentication rejection.
type Connector interface {
	Connect(context.Context, func(Presentation)) (Session, error)
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
