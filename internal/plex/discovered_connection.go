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
	path := filepath.Join(StateDir(c.StateDir), "server.json")
	if !interaction.SelectServer && !interaction.NewAccount {
		server, err := serverstate.LoadServer(path)
		if err == nil {
			result, err := c.connectRemembered(ctx, interaction, server)
			if err == nil || errors.Is(err, ErrSessionSave) || ctx.Err() != nil {
				return result, err
			}
			if !errors.Is(err, media.ErrUnauthorized) && !errors.Is(err, errDiscovery) {
				return c.recoverDiscovered(ctx, interaction, discovery, server)
			}
			// Missing or rejected server credentials require a fresh grant.
		} else if !errors.Is(err, os.ErrNotExist) {
			return connection.Session{}, ErrSessionSave
		}
	}
	servers, presentation, err := c.discoverServers(ctx, interaction, discovery)
	if err != nil {
		return connection.Session{}, err
	}
	if interaction.ChooseServer == nil {
		return connection.Session{}, errDiscovery
	}
	for {
		picker := presentation
		if len(servers) == 0 {
			picker.Message += "\nNo reachable servers. Scan again or use another account."
		}
		interaction.Show(picker)
		server, err := interaction.ChooseServer(ctx, servers)
		if errors.Is(err, connection.ErrRescan) {
			interaction.Show(connection.Presentation{Kind: connection.SetupConnecting, Title: "Finding Plex servers", Message: "Refreshing the server list.", BackToServers: true})
			servers, err = c.refreshServers(ctx, discovery)
			if err != nil {
				return connection.Session{}, err
			}
			continue
		}
		if err != nil {
			return connection.Session{}, err
		}
		return c.connectSelected(ctx, discovery, server, presentation.Recovered)
	}
}

// discoverServers validates the linked account and obtains fresh server grants.
// Account or network failures leave the remembered server and its token intact.
func (c Connector) discoverServers(ctx context.Context, interaction connection.Interaction, discovery *serverDiscovery) ([]connection.Server, connection.Presentation, error) {
	account := discovery.account
	dir := StateDir(c.StateDir)
	saved, recovered, err := loadDiscoveryAccount(dir, account.accountURL)
	if err != nil {
		return nil, connection.Presentation{}, ErrSessionSave
	}
	account.Session = saved
	if interaction.NewAccount {
		account.Session.Token, account.Session.UserID = "", ""
	}
	var user accountIdentity
	if account.Session.Token != "" {
		user, err = account.accountUser(ctx)
		if err != nil && !Rejected(err) {
			return nil, connection.Presentation{}, err
		}
	}
	if account.Session.Token == "" || Rejected(err) {
		err = account.linkAccount(ctx, func(code string) {
			interaction.Show(connection.Presentation{Kind: connection.SetupApproval, Title: "Link Plex", Code: code, Recovered: recovered, Retry: "New code", Message: "Open plex.tv/link with the account you want to use.\nEnter this code to approve MiSTerVision.", BackToServers: interaction.NewAccount})
		})
		if err != nil {
			return nil, connection.Presentation{}, err
		}
		user, err = account.accountUser(ctx)
		if err != nil {
			return nil, connection.Presentation{}, err
		}
	}
	account.Session.UserID = strconv.Itoa(user.ID)
	if err := ctx.Err(); err != nil {
		return nil, connection.Presentation{}, err
	}
	interaction.Show(connection.Presentation{Kind: connection.SetupConnecting, Title: "Finding Plex servers", Message: "Signed in as " + user.name() + ".\nChecking available server connections."})
	servers, err := c.refreshServers(ctx, discovery)
	if err != nil {
		return nil, connection.Presentation{}, err
	}
	presentation := connection.Presentation{Kind: connection.SetupServers, Title: "Choose a Plex server", Message: "Signed in as " + user.name() + ".", Recovered: recovered, SignIn: "Sign in with another account"}
	return servers, presentation, nil
}

// refreshServers keeps account linking separate from rescanning. An empty
// account can still choose a different sign-in from the server picker.
func (c Connector) refreshServers(ctx context.Context, discovery *serverDiscovery) ([]connection.Server, error) {
	servers, err := discovery.Discover(ctx)
	c.Diagnostics.Record("connection.discovery", slog.String("provider", "plex"), slog.Int("servers", len(servers)), slog.Bool("failed", err != nil))
	if (errors.Is(err, errNoServers) || errors.Is(err, errServersUnreachable)) && discovery.previous == nil {
		return nil, nil
	}
	if err != nil {
		return nil, errors.Join(errDiscovery, err)
	}
	return servers, nil
}

// connectSelected rechecks identity after confirmation and publishes the saved
// address only after authenticated access and credential storage both succeed.
func (c Connector) connectSelected(ctx context.Context, discovery *serverDiscovery, server connection.Server, recovered bool) (connection.Session, error) {
	account := discovery.account
	dir := StateDir(c.StateDir)
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
	session := serverstate.Session{Server: server.URL, ServerID: server.ID, DeviceID: account.Session.DeviceID, Token: token, UserID: account.Session.UserID}
	client := NewClient(config, session)
	client.Version, client.Diagnostics = c.Version, c.Diagnostics
	if err := client.validate(ctx); err != nil {
		return connection.Session{}, err
	}
	if err := ctx.Err(); err != nil {
		return connection.Session{}, err
	}
	state := discoveryState{Server: server, Account: &account.Session, Credentials: &client.Session}
	if err := saveDiscoveryState(dir, state); err != nil {
		return connection.Session{}, err
	}
	return connection.Session{Server: client, Recovered: recovered}, nil
}

// connectRemembered validates the saved media-server token without requiring
// plex.tv. A failed network or identity check triggers one recovery attempt.
func (c Connector) connectRemembered(ctx context.Context, interaction connection.Interaction, server connection.Server) (connection.Session, error) {
	interaction.Show(connection.Presentation{Kind: connection.SetupConnecting, Title: "Connecting to Plex", Message: "Opening the remembered server.", BackToServers: true})
	dir := StateDir(c.StateDir)
	state, err := loadDiscoveryState(dir)
	if err != nil {
		return connection.Session{}, ErrSessionSave
	}
	var saved serverstate.Session
	var recovered bool
	if state.Credentials != nil {
		saved = *state.Credentials
	} else {
		saved, recovered, err = serverstate.LoadSession(discoveredSessionDir(dir, server), server.URL)
		if err != nil {
			return connection.Session{}, ErrSessionSave
		}
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

// discoveredSessionDir locates legacy credentials stored per server address.
// New selections use discoveryState. Identifiers never become path components.
func discoveredSessionDir(dir string, server connection.Server) string {
	digest := sha256.Sum256([]byte(server.ID + "\x00" + server.URL))
	return filepath.Join(dir, "servers", fmt.Sprintf("%x", digest[:16]))
}
