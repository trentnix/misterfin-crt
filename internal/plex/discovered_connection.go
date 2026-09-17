package plex

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"

	"mistervision/internal/connection"
	"mistervision/internal/media"
	"mistervision/internal/serverstate"
)

// connectDiscovered keeps account linking separate from server credentials.
// A saved server reconnects directly. Fresh selection is committed only after
// authenticated access succeeds, so cancellation leaves the working choice intact.
func (c Connector) connectDiscovered(ctx context.Context, interaction connection.Interaction, discovery *serverDiscovery) (connection.Session, error) {
	account := discovery.account
	dir := StateDir(c.StateDir)
	path := filepath.Join(dir, "server.json")
	if !interaction.SelectServer {
		server, err := serverstate.LoadServer(path)
		if err == nil {
			result, err := c.connectRemembered(ctx, interaction, server)
			if err == nil || !errors.Is(err, media.ErrUnauthorized) && !errors.Is(err, errDiscovery) {
				return result, err
			}
			// Missing or rejected server credentials require a fresh grant.
			// A network failure leaves the saved connection intact for Retry.
		} else if !errors.Is(err, os.ErrNotExist) {
			return connection.Session{}, ErrSessionSave
		}
	}
	saved, recovered, err := serverstate.LoadSession(filepath.Join(dir, "account"), account.accountURL)
	if err != nil {
		return connection.Session{}, ErrSessionSave
	}
	account.Session = saved
	var user accountIdentity
	if saved.Token != "" {
		user, err = account.accountUser(ctx)
		if err != nil && !Rejected(err) {
			return connection.Session{}, err
		}
	}
	if saved.Token == "" || Rejected(err) {
		err = account.linkAccount(ctx, func(code string) {
			interaction.Show(connection.Presentation{Kind: connection.SetupApproval, Title: "Link Plex", Code: code, Recovered: recovered, Retry: "New code", Message: "Open plex.tv/link in a signed-in browser.\nEnter this code to approve MiSTerVision."})
		})
		if err != nil {
			return connection.Session{}, err
		}
		user, err = account.accountUser(ctx)
		if err != nil {
			return connection.Session{}, err
		}
	}
	account.Session.UserID = strconv.Itoa(user.ID)
	if err := ctx.Err(); err != nil {
		return connection.Session{}, err
	}
	if err := account.save(filepath.Join(dir, "account")); err != nil {
		return connection.Session{}, err
	}
	interaction.Show(connection.Presentation{Kind: connection.SetupConnecting, Title: "Finding Plex servers", Message: "Signed in as " + user.name() + ".\nChecking available server connections."})
	servers, err := discovery.Discover(ctx)
	c.Diagnostics.Record("connection.discovery", slog.String("provider", "plex"), slog.Int("servers", len(servers)), slog.Bool("failed", err != nil))
	if err != nil {
		return connection.Session{}, errors.Join(errDiscovery, err)
	}
	if interaction.ChooseServer == nil {
		return connection.Session{}, errDiscovery
	}
	interaction.Show(connection.Presentation{Kind: connection.SetupServers, Title: "Choose a Plex server", Message: "Signed in as " + user.name() + ".", Recovered: recovered})
	server, err := interaction.ChooseServer(ctx, servers)
	if err != nil {
		return connection.Session{}, err
	}
	token, ok := discovery.grants[server]
	if !ok {
		return connection.Session{}, errDiscovery
	}
	// Verify again after the user chooses. Selection may remain open while the
	// network changes. The account token is never sent to the selected server.
	if err := account.verifyIdentity(ctx, server); err != nil {
		return connection.Session{}, err
	}
	config := c.Config
	config.Server, config.InsecureTLS = server.URL, false
	session := serverstate.Session{Server: server.URL, ServerID: server.ID, DeviceID: saved.DeviceID, Token: token, UserID: account.Session.UserID}
	client := NewClient(config, session)
	client.Version, client.Diagnostics = c.Version, c.Diagnostics
	if err := client.validate(ctx); err != nil {
		return connection.Session{}, err
	}
	if err := ctx.Err(); err != nil {
		return connection.Session{}, err
	}
	if err := client.save(discoveredSessionDir(dir, server)); err != nil {
		return connection.Session{}, err
	}
	if err := serverstate.SaveServer(path, server); err != nil {
		return connection.Session{}, ErrSessionSave
	}
	return connection.Session{Server: client, Recovered: recovered}, nil
}

// connectRemembered validates the saved media-server token without requiring
// plex.tv. Address recovery and automatic endpoint refresh are separate work.
func (c Connector) connectRemembered(ctx context.Context, interaction connection.Interaction, server connection.Server) (connection.Session, error) {
	interaction.Show(connection.Presentation{Kind: connection.SetupConnecting, Title: "Connecting to Plex", Message: "Opening the remembered server.", BackToServers: true})
	saved, recovered, err := serverstate.LoadSession(discoveredSessionDir(StateDir(c.StateDir), server), server.URL)
	if err != nil {
		return connection.Session{}, ErrSessionSave
	}
	if saved.Token == "" || saved.UserID == "" || saved.ServerID != server.ID {
		return connection.Session{}, errDiscovery
	}
	config := c.Config
	config.Server, config.InsecureTLS = server.URL, false
	client := NewClient(config, saved)
	client.Version, client.Diagnostics = c.Version, c.Diagnostics
	if err := client.verifyIdentity(ctx, server); err != nil {
		return connection.Session{}, err
	}
	if err := client.validate(ctx); err != nil {
		return connection.Session{}, err
	}
	return connection.Session{Server: client, Recovered: recovered}, nil
}

// discoveredSessionDir preserves credentials for previous server addresses if
// saving a new selection fails. Untrusted identifiers never become path components.
func discoveredSessionDir(dir string, server connection.Server) string {
	digest := sha256.Sum256([]byte(server.ID + "\x00" + server.URL))
	return filepath.Join(dir, "servers", fmt.Sprintf("%x", digest[:16]))
}
