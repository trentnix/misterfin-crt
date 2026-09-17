// Package connection adapts Jellyfin sign-in and setup instructions to the
// browser's connection contract. Application assembly supplies file locations.
package connection

import (
	"context"
	"errors"

	"mistervision/internal/connection"
	"mistervision/internal/diagnostics"
	"mistervision/internal/jellyfin"
	jellyfinremote "mistervision/internal/jellyfin/remote"
	"mistervision/internal/serverstate"
)

// Connector uses explicit configuration or resolves a remembered/discovered
// server when the legacy file is absent. Each attempt reloads file-based state.
// Calls sharing StateDir must be serialized. Diagnostics may be nil.
type Connector struct {
	// Config is an immutable validated snapshot. Nil selects legacy ConfigPath.
	Config                        *jellyfin.Config
	ConfigPath, StateDir, Version string
	Diagnostics                   *diagnostics.Log
	// Discovery is used only when Config is nil and the legacy file is absent.
	Discovery connection.Discoverer
	// DiscoveryOnly is an explicit setup choice that ignores connection files.
	// Automatic startup leaves it false so configured addresses still win.
	DiscoveryOnly bool
	// SettingsPath identifies the JSON file in discovery recovery instructions.
	SettingsPath string
}

var _ connection.Connector = Connector{}

// Connect authenticates an account and supplies its media and remote services.
// Progress exposes only the public approval code, never the sign-in secret.
func (c Connector) Connect(ctx context.Context, interaction connection.Interaction) (connection.Session, error) {
	config, remembered, err := c.resolveConfig(ctx, interaction)
	if err != nil {
		return connection.Session{}, err
	}
	id := ""
	if remembered != nil {
		id = remembered.ID
	}
	saved, recovered, err := serverstate.LoadSessionForServer(c.StateDir, config.Server, id)
	if err != nil {
		return connection.Session{}, &connectionError{connectionSession, err}
	}
	client, err := c.authenticate(ctx, interaction, config, saved, recovered, id)
	if err != nil && remembered != nil && errors.Is(err, jellyfin.ErrServerUnavailable) && ctx.Err() == nil {
		client, err = c.recoverAddress(ctx, interaction, config, *remembered, saved, recovered)
	}
	if err != nil {
		return connection.Session{}, err
	}
	return connection.Session{Server: client, Remote: jellyfinremote.New(client), Recovered: recovered}, nil
}

// authenticate validates a moved endpoint before using credentials from another
// address. Stable identity metadata also makes interrupted address saves recoverable.
func (c Connector) authenticate(ctx context.Context, interaction connection.Interaction, config jellyfin.Config, saved jellyfin.Session, recovered bool, id string) (*jellyfin.Client, error) {
	if saved.Server != config.Server {
		if err := jellyfin.VerifyServer(ctx, config.Server, id); err != nil {
			return nil, &connectionError{connectionAuthentication, err}
		}
	}
	changed := saved.Server != config.Server || saved.ServerID != id
	saved.Server, saved.ServerID = config.Server, id
	client := jellyfin.NewClient(config, saved)
	client.Version, client.Diagnostics = c.Version, c.Diagnostics
	if recovered {
		c.Diagnostics.Record("authentication.session-recovered")
	}
	err := client.Authenticate(ctx, c.StateDir, func(code string) { interaction.Show(approval(code, recovered)) })
	if err != nil {
		return nil, &connectionError{connectionAuthentication, err}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if changed {
		if err := jellyfin.SaveSession(c.StateDir, client.Session); err != nil {
			return nil, &connectionError{connectionSession, err}
		}
	}
	return client, nil
}

// connectionError retains the failed operation without adding sensitive text.
type connectionError struct {
	stage connectionStage
	err   error
}

func (e *connectionError) Error() string { return e.err.Error() }
func (e *connectionError) Unwrap() error { return e.err }
