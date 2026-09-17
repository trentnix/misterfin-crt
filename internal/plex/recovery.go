package plex

import (
	"context"
	"errors"
	"log/slog"

	"mistervision/internal/connection"
)

var errRecovery = errors.New("remembered Plex server could not be recovered")

// recoverDiscovered refreshes account and LAN addresses once after a remembered
// connection fails. The old identity and HTTPS policy constrain discovery before
// probing, so another server or a transport downgrade cannot become the choice.
func (c Connector) recoverDiscovered(ctx context.Context, interaction connection.Interaction, discovery *serverDiscovery, old connection.Server) (connection.Session, error) {
	interaction.Show(connection.Presentation{Kind: connection.SetupConnecting, Title: "Finding your Plex server", Message: "The saved address is unavailable.\nChecking for a new address.", BackToServers: true})
	retry := *discovery
	retry.previous = &old
	servers, presentation, err := c.discoverServers(ctx, interaction, &retry)
	c.Diagnostics.Record("connection.rediscovery", slog.String("provider", "plex"), slog.Int("servers", len(servers)), slog.Bool("failed", err != nil))
	if ctx.Err() != nil {
		return connection.Session{}, ctx.Err()
	}
	if err != nil {
		return connection.Session{}, errors.Join(errRecovery, err)
	}
	if len(servers) == 0 || interaction.ChooseServer == nil {
		return connection.Session{}, errRecovery
	}
	interaction.Show(connection.Presentation{Kind: connection.SetupServers, Title: "Server address changed", Message: "Same server found at a new address.\nSelect to reconnect, or go back.", Recovered: presentation.Recovered})
	selected, err := interaction.ChooseServer(ctx, servers)
	if err != nil {
		return connection.Session{}, err
	}
	result, err := c.connectSelected(ctx, &retry, selected, presentation.Recovered)
	if err != nil {
		return connection.Session{}, err
	}
	c.Diagnostics.Record("connection.address-recovered", slog.String("provider", "plex"))
	return result, nil
}
