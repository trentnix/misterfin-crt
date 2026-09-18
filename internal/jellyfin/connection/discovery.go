package connection

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"

	"mistervision/internal/connection"
	"mistervision/internal/jellyfin"
	"mistervision/internal/serverstate"
)

var errNoServers = errors.New("no Jellyfin servers found")

// resolveConfig preserves explicit configuration, including its errors. Only a
// missing legacy file permits a remembered selection or network discovery.
func (c *Connector) resolveConfig(ctx context.Context, interaction connection.Interaction) (jellyfin.Config, *connection.Server, error) {
	if c.Config != nil {
		return *c.Config, nil, nil
	}
	config := jellyfin.Config{Transcode: jellyfin.DefaultTranscodeProfile()}
	err := os.ErrNotExist
	if !c.DiscoveryOnly {
		config, err = jellyfin.LoadConfig(c.ConfigPath)
	}
	if err == nil {
		return config, nil, nil
	}
	if !errors.Is(err, os.ErrNotExist) || c.Discovery == nil {
		return config, nil, &connectionError{connectionConfig, err}
	}
	if interaction.SelectServer {
		c.selected = nil
	}
	if c.selected != nil {
		config.Server = c.selected.URL
		return config, c.selected, nil
	}
	path := filepath.Join(c.StateDir, "jellyfin-server.json")
	var server connection.Server
	var remembered *connection.Server
	if !interaction.SelectServer {
		server, err = serverstate.LoadServer(path)
		if err == nil {
			saved := server
			remembered = &saved
		}
	}
	if !interaction.SelectServer && err != nil && !errors.Is(err, os.ErrNotExist) {
		return config, nil, &connectionError{connectionServerStorage, err}
	}
	if interaction.SelectServer || errors.Is(err, os.ErrNotExist) {
		interaction.Show(connection.Presentation{Kind: connection.SetupConnecting, Title: "Finding Jellyfin servers", Message: "Looking on your local network."})
		servers, err := c.Discovery.Discover(ctx)
		c.Diagnostics.Record("connection.discovery", slog.Int("servers", len(servers)), slog.Bool("failed", err != nil))
		if err != nil {
			return config, nil, &connectionError{connectionDiscovery, err}
		}
		if len(servers) == 0 {
			return config, nil, &connectionError{connectionDiscovery, errNoServers}
		}
		if interaction.ChooseServer == nil {
			return config, nil, &connectionError{connectionDiscovery, errors.New("server selection is unavailable")}
		}
		server, err = interaction.ChooseServer(ctx, servers)
		if err != nil {
			return config, nil, err
		}
		// The UI must return an offered candidate, not an arbitrary address.
		found := false
		for _, candidate := range servers {
			if candidate == server {
				found = true
				break
			}
		}
		if !found {
			return config, nil, &connectionError{connectionDiscovery, errors.New("invalid server selection")}
		}
		if err := ctx.Err(); err != nil {
			return config, nil, err
		}
		// Selecting a server does not replace the working sign-in. Connect
		// publishes the address only after this attempt authenticates.
		c.selected = &server
		remembered = c.selected
	}
	// A remembered discovery choice can be changed during sign-in too. Publish
	// navigation before loading credentials so failures retain the Back action.
	progress := c.Describe(nil)
	progress.BackToServers = true
	interaction.Show(progress)
	config.Server = server.URL
	return config, remembered, nil
}
