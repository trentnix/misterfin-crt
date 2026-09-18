package connection

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"path/filepath"

	"mistervision/internal/connection"
	"mistervision/internal/jellyfin"
	"mistervision/internal/serverstate"
)

var errServerNotFound = errors.New("remembered Jellyfin server was not rediscovered")

// recoverAddress offers only verified addresses for the remembered identity.
// It runs once per failed remembered connection, never for explicit configuration.
// The user must accept the candidate before credentials go to its new address.
func (c *Connector) recoverAddress(ctx context.Context, interaction connection.Interaction, config jellyfin.Config, old connection.Server, saved jellyfin.Session, recovered bool) (connection.Session, connection.Server, error) {
	interaction.Show(connection.Presentation{Kind: connection.SetupConnecting, Title: "Finding your Jellyfin server", Message: connection.AddressRecoveryMessage, Back: connection.BackServers})
	servers, err := c.Discovery.Discover(ctx)
	c.Diagnostics.Record("connection.rediscovery", slog.Bool("failed", err != nil), slog.Int("servers", len(servers)))
	if err != nil {
		return connection.Session{}, connection.Server{}, &connectionError{connectionRecovery, err}
	}
	var candidates []connection.Server
	oldURL, _ := url.Parse(old.URL)
	for _, server := range servers {
		if server.Validate() != nil || server.ID != old.ID || server.URL == old.URL {
			continue
		}
		candidateURL, _ := url.Parse(server.URL)
		if oldURL.Scheme == "https" && candidateURL.Scheme != "https" {
			continue
		}
		if err := jellyfin.VerifyServer(ctx, server.URL, old.ID); err == nil {
			candidates = append(candidates, server)
		}
	}
	if err := ctx.Err(); err != nil {
		return connection.Session{}, connection.Server{}, err
	}
	if len(candidates) == 0 || interaction.ChooseServer == nil {
		return connection.Session{}, connection.Server{}, &connectionError{connectionRecovery, errServerNotFound}
	}
	interaction.Show(connection.Presentation{Kind: connection.SetupServers, Title: "Server address changed", Message: "Same server found at a new address.\nSelect to reconnect, or go back."})
	selected, err := interaction.ChooseServer(ctx, candidates)
	if err != nil {
		return connection.Session{}, connection.Server{}, err
	}
	offered := false
	for _, candidate := range candidates {
		if candidate == selected {
			offered = true
			break
		}
	}
	if !offered {
		return connection.Session{}, connection.Server{}, &connectionError{connectionRecovery, errors.New("invalid recovery selection")}
	}
	if err := ctx.Err(); err != nil {
		return connection.Session{}, connection.Server{}, err
	}
	interaction.Show(connection.Presentation{Kind: connection.SetupConnecting, Title: "Connecting to Jellyfin", Message: "Reconnecting at the new address.", Back: connection.BackServers})
	config.Server = selected.URL
	result, err := c.authenticate(ctx, interaction, config, saved, recovered, selected.ID)
	if err != nil {
		return connection.Session{}, connection.Server{}, err
	}
	// Sign-in is saved first with the stable ID. If the address save fails or
	// is interrupted, the next attempt can rediscover without discarding tokens.
	if err := serverstate.SaveServer(filepath.Join(c.StateDir, "jellyfin-server.json"), selected); err != nil {
		return connection.Session{}, connection.Server{}, &connectionError{connectionServerStorage, err}
	}
	c.Diagnostics.Record("connection.address-recovered")
	return result, selected, nil
}
