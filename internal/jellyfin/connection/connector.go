// Package connection adapts Jellyfin sign-in and setup instructions to the
// browser's connection contract. Application assembly supplies file locations.
package connection

import (
	"context"
	"errors"
	"path/filepath"

	"mistervision/internal/connection"
	"mistervision/internal/diagnostics"
	"mistervision/internal/jellyfin"
	"mistervision/internal/serverstate"
)

// Connector uses explicit configuration or resolves a remembered/discovered
// server when the legacy file is absent. A selected server stays in memory for
// sign-in retries until authentication succeeds. Calls sharing a Connector or
// StateDir must be serialized. Diagnostics may be nil.
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
	// selected is tentative discovery state. Back requests a fresh selection.
	selected *connection.Server
}

var _ connection.Connector = (*Connector)(nil)

// Connect authenticates an account and supplies its media and remote services.
// Progress exposes only the public approval code, never the sign-in secret.
func (c *Connector) Connect(ctx context.Context, interaction connection.Interaction) (connection.Session, error) {
	if interaction.Reauthenticate {
		c.selected = nil
	}
	config, remembered, err := c.resolveConfig(ctx, interaction)
	if err != nil {
		return connection.Session{}, err
	}
	endpoint := connection.Server{}
	if remembered != nil {
		endpoint = *remembered
	}
	id := endpoint.ID
	saved, recovered, err := serverstate.LoadSessionForServer(c.StateDir, config.Server, id)
	if err != nil {
		return connection.Session{}, &connectionError{connectionSession, err}
	}
	if interaction.ProfileAction == connection.ProfileForget && config.APIKey == "" {
		users, err := loadUsers(c.StateDir)
		if err != nil {
			return connection.Session{}, &connectionError{connectionSession, err}
		}
		if users == nil {
			return connection.Session{}, &connectionError{connectionSession, errUserState}
		}
		if _, found := users.find(saved, saved.UserID); !found {
			// Retry cleanup after the roster committed but the active-file write failed.
			if err := jellyfin.SaveSession(c.StateDir, newUserSession(saved)); err != nil {
				return connection.Session{}, errors.Join(connection.ErrSignedOut, &connectionError{connectionSession, err})
			}
			return connection.Session{}, connection.ErrSignedOut
		}
		_, err = c.forgetUser(ctx, interaction, saved, &users, saved.UserID)
		if err == nil {
			err = connection.ErrCanceled
		}
		return connection.Session{}, err
	}
	result, err := c.authenticate(ctx, interaction, config, saved, recovered, id)
	if err != nil && remembered != nil && c.selected == nil && errors.Is(err, jellyfin.ErrServerUnavailable) && ctx.Err() == nil {
		result, endpoint, err = c.recoverAddress(ctx, interaction, config, *remembered, saved, recovered)
	}
	if err != nil {
		return connection.Session{}, err
	}
	if c.selected != nil {
		if err := serverstate.SaveServer(filepath.Join(c.StateDir, "jellyfin-server.json"), *c.selected); err != nil {
			return connection.Session{}, &connectionError{connectionServerStorage, err}
		}
		c.selected = nil
	}
	result.Endpoint = endpoint
	return result, nil
}

// connectionError retains the failed operation without adding sensitive text.
type connectionError struct {
	stage connectionStage
	err   error
}

func (e *connectionError) Error() string { return e.err.Error() }
func (e *connectionError) Unwrap() error { return e.err }
