package connection

import (
	"context"
	"errors"
)

// Retained keeps one authenticated account available while another connection
// is active. Connect calls must be serialized. Remote control is still run only
// by the active browser. Describe never reads or changes the cached account.
type Retained struct {
	Connector Connector
	// Remember commits the successful selection using public endpoint metadata.
	// It runs for fresh and retained sessions. Nil disables persistence.
	Remember func(Server) error
	session  Session
	previous *Retained // A tentative selection replaces this cache only after success.
}

var errRemember = errors.New("cannot remember connection")

// Connect reuses an authenticated account until setup requests a new server or
// reauthentication. Failed authentication never replaces another account.
func (c *Retained) Connect(ctx context.Context, i Interaction) (Session, error) {
	if c.previous != nil && c.session.Server != nil {
		// Once promoted, both routes use the same cache, including invalidation.
		return c.previous.Connect(ctx, i)
	}
	if err := ctx.Err(); err != nil {
		return Session{}, err
	}
	if i.SelectServer || i.Reauthenticate || i.NewAccount || i.SelectProfile {
		c.session = Session{}
	}
	session := c.session
	if session.Server == nil {
		var err error
		session, err = c.Connector.Connect(ctx, i)
		if err != nil {
			return Session{}, err
		}
	}
	if err := ctx.Err(); err != nil {
		return Session{}, err
	}
	if c.Remember != nil {
		if err := c.Remember(session.Endpoint); err != nil {
			return Session{}, errRemember
		}
	}
	c.session = session
	c.session.Recovered = false
	if c.previous != nil {
		c.previous.session = c.session
	}
	return session, nil
}

// Describe preserves provider-specific setup instructions and reports storage
// failures without exposing server addresses or credentials.
func (c *Retained) Describe(err error) Presentation {
	if errors.Is(err, errRemember) {
		return Presentation{Kind: SetupFailure, Title: "Can't remember connection", Message: "Make sure the sign-in folder is writable, then retry.", Retry: "Retry"}
	}
	return c.Connector.Describe(err)
}

// NewSelection starts tentative setup without discarding the working account.
// Retries use the new selection's own cache. Only successful authentication and
// persistence replace the original cache. Calls on both must be serialized.
func (c *Retained) NewSelection() *Retained {
	return &Retained{Connector: c.Connector, Remember: c.Remember, previous: c}
}
