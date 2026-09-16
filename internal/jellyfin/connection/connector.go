// Package connection adapts Jellyfin sign-in and setup instructions to the
// browser's connection contract. Application assembly supplies file locations.
package connection

import (
	"context"

	"misterfin-crt/internal/connection"
	"misterfin-crt/internal/diagnostics"
	"misterfin-crt/internal/jellyfin"
	jellyfinremote "misterfin-crt/internal/jellyfin/remote"
)

// Connector reloads Jellyfin configuration and saved sign-in on each attempt.
// Calls sharing StateDir must be serialized. Diagnostics may be nil.
type Connector struct {
	ConfigPath, StateDir, Version string
	Diagnostics                   *diagnostics.Log
}

var _ connection.Connector = Connector{}

// Connect authenticates an account and supplies its media and remote services.
// Progress exposes only the public approval code, never the sign-in secret.
func (c Connector) Connect(ctx context.Context, progress func(connection.Presentation)) (connection.Session, error) {
	config, err := jellyfin.LoadConfig(c.ConfigPath)
	if err != nil {
		return connection.Session{}, &connectionError{connectionConfig, err}
	}
	saved, recovered, err := jellyfin.LoadSession(c.StateDir, config.Server)
	if err != nil {
		return connection.Session{}, &connectionError{connectionSession, err}
	}
	client := jellyfin.NewClient(config, saved)
	client.Version, client.Diagnostics = c.Version, c.Diagnostics
	if recovered {
		c.Diagnostics.Record("authentication.session-recovered")
	}
	err = client.Authenticate(ctx, c.StateDir, func(code string) {
		if progress != nil {
			progress(approval(code, recovered))
		}
	})
	if err != nil {
		return connection.Session{}, &connectionError{connectionAuthentication, err}
	}
	return connection.Session{Server: client, Remote: jellyfinremote.New(client), Recovered: recovered}, nil
}

// connectionError retains the failed operation without adding sensitive text.
type connectionError struct {
	stage connectionStage
	err   error
}

func (e *connectionError) Error() string { return e.err.Error() }
func (e *connectionError) Unwrap() error { return e.err }
