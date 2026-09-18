package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"mistervision/internal/connection"
	"mistervision/internal/diagnostics"
	"mistervision/internal/jellyfin"
	jfconnection "mistervision/internal/jellyfin/connection"
	"mistervision/internal/plex"
	"mistervision/internal/serverstate"
	"mistervision/internal/settings"
)

// connectionCatalog owns provider assembly and the retained accounts for one
// application run. Browser sessions borrow a connector, never configuration files.
type connectionCatalog struct {
	mu          sync.RWMutex // Guards immutable menu snapshots published by sign-in workers.
	choices     []connection.Choice
	connectors  map[string]connection.Connector
	selected    string
	discoveries map[string]*connection.Retained
	retained    map[string]*connection.Retained
	notice      string
}

// newConnectionCatalog preserves automatic single-server startup and adds named
// connections. Selection state is separate from settings and invalidated when
// connection configuration changes. UI/display settings do not affect selection.
func newConnectionCatalog(source *settings.File, configPath, stateDir, version string, log *diagnostics.Log) (*connectionCatalog, error) {
	defaultConnector, err := serverConnector(source, configPath, stateDir, version, log)
	if err != nil {
		return nil, err
	}
	profiles, err := settings.ParseConnections(source.Section("connections"))
	if err != nil {
		return nil, err
	}
	catalog := &connectionCatalog{connectors: make(map[string]connection.Connector), discoveries: make(map[string]*connection.Retained), retained: make(map[string]*connection.Retained), selected: "default"}
	fingerprint := sha256.New()
	fingerprint.Write(source.Section("server").Data)
	fingerprint.Write([]byte{0})
	fingerprint.Write(source.Section("connections").Data)
	if source.Section("server").Data == nil {
		legacy := settings.Read(configPath, 64<<10, false)
		if legacy.Err == nil {
			fingerprint.Write(legacy.Data)
		}
	}
	digest := fmt.Sprintf("%x", fingerprint.Sum(nil))
	choicePath := filepath.Join(stateDir, "connection-choice.json")
	add := func(id string, connector connection.Connector) *connection.Retained {
		retained := &connection.Retained{Connector: connector, Remember: func(connection.Server) error {
			return serverstate.SaveChoice(choicePath, serverstate.Choice{ID: id, Configuration: digest})
		}}
		catalog.connectors[id] = retained
		catalog.retained[id] = retained
		return retained
	}
	defaultAccount := add("default", defaultConnector)
	var existing []connection.Choice
	_, legacyErr := os.Stat(configPath)
	_, savedErr := os.Stat(filepath.Join(stateDir, "jellyfin-server.json"))
	if source.Section("server").Data == nil && legacyErr != nil {
		catalog.observeDiscovery(defaultAccount, "default", "Jellyfin")
	}
	if source.Section("server").Data != nil || legacyErr == nil || savedErr == nil {
		name := "Configured server"
		provider := "Jellyfin"
		if c, e := settings.ParseServer(source.Section("server")); e == nil && c != nil && c.Provider == "plex" {
			provider = "Plex"
		}
		if source.Section("server").Data == nil && legacyErr != nil {
			name = "Saved Jellyfin server"
		}
		choice := connection.Choice{ID: "default", Name: name, Description: provider}
		existing = append(existing, choice)
	}
	for _, profile := range profiles {
		// A renamed label keeps the same account. A changed server gets isolated state.
		identity := profile.Server.Provider + "\x00" + profile.Server.URL
		if login := profile.Server.Jellyfin; login != nil {
			identity += "\x00" + login.Username + "\x00" + login.APIKey
		}
		account := sha256.Sum256([]byte(identity))
		dir := filepath.Join(stateDir, "connections", profile.ID+"-"+fmt.Sprintf("%x", account[:8]))
		id := "profile/" + profile.ID
		add(id, configuredConnector(profile.Server, source.Path, dir, version, log))
		provider := "Jellyfin"
		if profile.Server.Provider == "plex" {
			provider = "Plex"
		}
		choice := connection.Choice{ID: id, Name: profile.Name, Description: provider + " · " + profile.Server.URL}
		existing = append(existing, choice)
	}
	if source.Section("server").Data == nil && legacyErr != nil && savedErr != nil && len(profiles) > 0 {
		catalog.selected = "profile/" + profiles[0].ID
	}
	// Each provider's discovered account stays separate from explicitly
	// configured profiles. The new route is tentative until connection succeeds.
	var providers []connection.Choice
	addDiscovery := func(id, name, description, path string, connector connection.Connector) {
		catalog.discoveries[id] = add(id, connector)
		catalog.observeDiscovery(catalog.discoveries[id], id, name)
		catalog.startSelection(id + "-new")
		if server, err := serverstate.LoadServer(path); err == nil {
			existing = append(existing, connection.Choice{ID: id, Name: server.Name, Description: name + " · " + server.URL})
		}
		providers = append(providers, connection.Choice{ID: id + "-new", Name: name, Description: description})
	}
	discoveryDir := filepath.Join(stateDir, "discovery")
	jellyfinDir := filepath.Join(discoveryDir, "jellyfin")
	addDiscovery("jellyfin", "Jellyfin", "Find a server on your local network", filepath.Join(jellyfinDir, "jellyfin-server.json"),
		&jfconnection.Connector{Discovery: jellyfin.Discovery{}, DiscoveryOnly: true, SettingsPath: source.Path, ConfigPath: configPath, StateDir: jellyfinDir, Version: version, Diagnostics: log})
	addDiscovery("plex", "Plex", "Link your account and choose a server", filepath.Join(plex.StateDir(discoveryDir), "server.json"),
		plex.Connector{StateDir: discoveryDir, Version: version, Diagnostics: log})
	if len(existing) > 0 {
		catalog.choices = append(catalog.choices, connection.Choice{ID: "existing", Name: "Use existing connection", Description: "Choose a configured or remembered server", Children: existing})
	}
	catalog.choices = append(catalog.choices, providers...)
	saved, err := serverstate.LoadChoice(choicePath)
	if err == nil && saved.Configuration == digest {
		if _, ok := catalog.connectors[saved.ID]; ok {
			catalog.selected = saved.ID
		} else {
			catalog.notice = messageSavedConnectionUnavailable
			log.ConfigurationFallback("connections", "configured-startup", errors.New("unknown saved connection"))
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		catalog.notice = messageSavedConnectionUnreadable
		log.ConfigurationFallback("connections", "configured-startup", err)
	}
	return catalog, nil
}

// Choices returns the latest immutable menu snapshot. Sign-in workers publish
// replacements after a successful selection, so browsers never own the catalog.
func (c *connectionCatalog) Choices() []connection.Choice {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.choices
}

// observeDiscovery updates the menu only after provider state and the selected
// route have been saved. Metadata comes from the connector, not its private files.
func (c *connectionCatalog) observeDiscovery(account *connection.Retained, id, provider string) {
	remember := account.Remember
	account.Remember = func(server connection.Server) error {
		if err := server.Validate(); err != nil {
			return err
		}
		if err := remember(server); err != nil {
			return err
		}
		choice := connection.Choice{ID: id, Name: server.Name, Description: provider + " · " + server.URL}
		c.mu.Lock()
		defer c.mu.Unlock()
		choices := append([]connection.Choice(nil), c.choices...)
		group := -1
		for i, entry := range choices {
			if entry.ID == "existing" {
				group = i
				break
			}
		}
		if group < 0 {
			choices = append([]connection.Choice{{ID: "existing", Name: "Use existing connection", Description: "Choose a configured or remembered server"}}, choices...)
			group = 0
		}
		children := append([]connection.Choice(nil), choices[group].Children...)
		found := false
		for i, entry := range children {
			if entry.ID == id {
				children[i] = choice
				found = true
				break
			}
		}
		if !found {
			children = append(children, choice)
		}
		choices[group].Children = children
		c.choices = choices
		return nil
	}
}

// discoverConnection enters the existing discovery route with a fresh scan.
// The retained connector remembers the normal route after successful sign-in.
type discoverConnection struct {
	connector connection.Connector
	started   bool
}

// Connect forces the picker until a server has actually been selected.
func (c *discoverConnection) Connect(ctx context.Context, i connection.Interaction) (connection.Session, error) {
	if !c.started {
		i.SelectServer = true
		choose := i.ChooseServer
		if choose != nil {
			i.ChooseServer = func(ctx context.Context, servers []connection.Server) (connection.Server, error) {
				selected, err := choose(ctx, servers)
				if err == nil {
					c.started = true
				}
				return selected, err
			}
		}
	}
	return c.connector.Connect(ctx, i)
}

// Describe delegates safe progress and recovery text to the selected provider.
func (c *discoverConnection) Describe(err error) connection.Presentation {
	return c.connector.Describe(err)
}

// connectionID makes fresh selection and remembered startup share browser state.
func (c *connectionCatalog) connectionID(id string) string {
	id = strings.TrimPrefix(id, "viewer/")
	base := strings.TrimSuffix(id, "-new")
	if c.discoveries[base] != nil {
		return base
	}
	return id
}

// startSelection gives each new setup attempt an independent tentative cache.
// Failed or canceled setup leaves the previous retained connection available.
func (c *connectionCatalog) startSelection(id string) {
	base := c.connectionID(id)
	if base != id && c.discoveries[base] != nil {
		c.connectors[id] = &discoverConnection{connector: c.discoveries[base].NewSelection()}
	}
}

// startProfileSelection borrows a tentative cache for the active connection.
// Canceling returns to its retained session. Success promotes the new viewer.
func (c *connectionCatalog) startProfileSelection(id string, action connection.ProfileAction) string {
	route := "viewer/" + id
	c.connectors[route] = &profileConnection{connector: c.retained[id].NewSelection(), action: action}
	return route
}

// profileConnection requests a viewer only until this attempt connects.
type profileConnection struct {
	action    connection.ProfileAction
	connector connection.Connector
	connected bool
}

// Connect requests a fresh profile until the tentative session succeeds.
func (c *profileConnection) Connect(ctx context.Context, i connection.Interaction) (connection.Session, error) {
	if i.ProfileAction == connection.ProfileUnchanged && !c.connected {
		i.ProfileAction = c.action
	}
	if !c.connected {
		progress := c.Describe(nil)
		progress.Back = connection.BackConnection
		i.Show(progress)
	}
	session, err := c.connector.Connect(ctx, i)
	if err == nil {
		c.connected = true
	}
	return session, err
}

// Describe delegates safe setup instructions to the provider.
func (c *profileConnection) Describe(err error) connection.Presentation {
	return c.connector.Describe(err)
}
