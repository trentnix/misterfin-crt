package plex

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"strings"

	"mistervision/internal/connection"
	"mistervision/internal/serverstate"
)

// loadAccount migrates configured sign-in lazily. Account and server tokens are
// replaced together only after the selected viewer can use the media server.
func (c Connector) loadAccount(origin string) (serverstate.Session, bool, error) {
	dir := StateDir(c.StateDir)
	state, err := loadDiscoveryState(dir)
	if err == nil && state.Account != nil {
		return loadDiscoveryAccount(dir, origin)
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return serverstate.Session{}, false, err
	}
	if c.Config.Server == "" {
		return loadDiscoveryAccount(dir, origin)
	}
	saved, recovered, err := serverstate.LoadSession(dir, c.Config.Server)
	saved.Server = origin
	return saved, recovered, err
}

// configuredServer keeps the configured address authoritative. Home profiles
// receive only their resource grant, never the linking account's server access.
func (c Connector) configuredServer(ctx context.Context, d *serverDiscovery) ([]connection.Server, error) {
	d.grants = nil
	client := NewClient(c.Config, serverstate.Session{DeviceID: d.account.Session.DeviceID})
	data, _, err := client.fetch(ctx, client.HTTP, c.Config.Server, "", "GET", "/identity", nil)
	if err != nil {
		return nil, err
	}
	var identity struct {
		Container struct {
			ID string `json:"machineIdentifier"`
		} `json:"MediaContainer"`
	}
	if json.Unmarshal(data, &identity) != nil || identity.Container.ID == "" {
		return nil, errDiscovery
	}
	server := connection.Server{ID: identity.Container.ID, Name: "Plex server", URL: c.Config.Server}
	token := d.account.Session.Token
	data, _, err = d.account.fetch(ctx, d.account.accountHTTP, d.account.accountURL, token, "GET", "/api/v2/resources", url.Values{"includeHttps": {"1"}, "includeRelay": {"0"}})
	if err != nil {
		return nil, err
	}
	var resources []accountResource
	if json.Unmarshal(data, &resources) != nil || len(resources) > 256 {
		return nil, errDiscovery
	}
	token = ""
	for _, r := range resources {
		if r.ID == server.ID && strings.Contains(","+r.Provides+",", ",server,") {
			token = r.Token
			server.Name = r.Name
			break
		}
	}
	if token == "" {
		return nil, errNoServers
	}
	if server.Validate() != nil {
		return nil, errDiscovery
	}
	d.grants = map[connection.Server]string{server: token}
	return []connection.Server{server}, nil
}
