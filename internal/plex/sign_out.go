package plex

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"mistervision/internal/connection"
)

// signOut commits an empty authoritative record before removing legacy copies.
// The marker survives a crash or cleanup failure and prevents token fallback.
// Public server metadata stays available for reconnecting from Connections.
func (c Connector) signOut(ctx context.Context, i connection.Interaction) error {
	dir := StateDir(c.StateDir)
	state, err := loadDiscoveryState(dir)
	if err != nil {
		return ErrSessionSave
	}
	if !state.SignedOut {
		name := state.AccountName
		if name == "" {
			name = "the linked Plex account"
		}
		accepted, err := i.Ask(ctx, connection.Confirmation{Title: "Sign out of Plex?", Message: fmt.Sprintf(messageSignOut, name), Accept: "Sign out"})
		if err != nil {
			return err
		}
		if !accepted {
			return connection.ErrCanceled
		}
		if err := saveDiscoveryState(dir, discoveryState{Server: state.Server, SignedOut: true}); err != nil {
			return err
		}
	}
	// These paths hold only this connection's legacy credentials. Settings and
	// per-viewer artwork/playback preferences live outside this private store.
	for _, name := range []string{"account", "servers", "session.json"} {
		if err := os.RemoveAll(filepath.Join(dir, name)); err != nil {
			return errors.Join(connection.ErrSignedOut, ErrSessionSave)
		}
	}
	return connection.ErrSignedOut
}
