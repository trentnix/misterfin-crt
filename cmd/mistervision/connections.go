package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"mistervision/internal/connection"
	"mistervision/internal/diagnostics"
	"mistervision/internal/jellyfin"
	jfconnection "mistervision/internal/jellyfin/connection"
	"mistervision/internal/serverstate"
	"mistervision/internal/settings"
)

// connectionCatalog owns provider assembly and the retained accounts for one
// application run. Browser sessions borrow a connector, never configuration files.
type connectionCatalog struct {
	choices    []connection.Choice
	connectors map[string]connection.Connector
	selected   string
	discovery  *connection.Retained
	notice     string
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
	catalog := &connectionCatalog{connectors: make(map[string]connection.Connector), selected: "default"}
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
		retained := &connection.Retained{Connector: connector, Remember: func() error {
			return serverstate.SaveChoice(choicePath, serverstate.Choice{ID: id, Configuration: digest})
		}}
		catalog.connectors[id] = retained
		return retained
	}
	add("default", defaultConnector)
	var existing, plexChoices []connection.Choice
	_, legacyErr := os.Stat(configPath)
	_, savedErr := os.Stat(filepath.Join(stateDir, "jellyfin-server.json"))
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
		if provider == "Plex" {
			plexChoices = append(plexChoices, choice)
		}
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
		if provider == "Plex" {
			plexChoices = append(plexChoices, choice)
		}
	}
	if source.Section("server").Data == nil && legacyErr != nil && savedErr != nil && len(profiles) > 0 {
		catalog.selected = "profile/" + profiles[0].ID
	}
	// Discovery uses a separate account folder so selecting another Jellyfin
	// server does not replace credentials for the configured connection.
	discoveryDir := filepath.Join(stateDir, "discovery", "jellyfin")
	discovery := jfconnection.Connector{Discovery: jellyfin.Discovery{}, DiscoveryOnly: true, SettingsPath: source.Path, ConfigPath: configPath, StateDir: discoveryDir, Version: version, Diagnostics: log}
	catalog.discovery = add("jellyfin", discovery)
	if _, err := os.Stat(filepath.Join(discoveryDir, "jellyfin-server.json")); err == nil {
		existing = append(existing, connection.Choice{ID: "jellyfin", Name: "Discovered Jellyfin server", Description: "Use the remembered server and sign-in"})
	}
	if len(existing) > 0 {
		catalog.choices = append(catalog.choices, connection.Choice{ID: "existing", Name: "Use existing connection", Description: "Choose a configured or remembered server", Children: existing})
	}
	catalog.choices = append(catalog.choices, connection.Choice{ID: "jellyfin-new", Name: "Jellyfin", Description: "Find a server on your local network"})
	// The new-server route forces discovery once, then becomes the remembered route.
	catalog.connectors["jellyfin-new"] = &discoverConnection{connector: catalog.discovery.NewSelection()}
	plex := connection.Choice{Name: "Plex", Description: "Choose a configured Plex server", Children: plexChoices}
	if len(plexChoices) == 0 {
		plex.Description = "Add a Plex server in settings.json"
		plex.Help = "Add a connection with provider plex and its server URL. Restart MiSTerVision to load the new configuration."
	}
	catalog.choices = append(catalog.choices, plex)
	saved, err := serverstate.LoadChoice(choicePath)
	if err == nil && saved.Configuration == digest {
		if _, ok := catalog.connectors[saved.ID]; ok {
			catalog.selected = saved.ID
		} else {
			catalog.notice = "Saved connection is unavailable. Using configured startup."
			log.ConfigurationFallback("connections", "configured-startup", errors.New("unknown saved connection"))
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		catalog.notice = "Saved connection choice could not be read. Using configured startup."
		log.ConfigurationFallback("connections", "configured-startup", err)
	}
	return catalog, nil
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

// Describe delegates safe progress and recovery text to Jellyfin.
func (c *discoverConnection) Describe(err error) connection.Presentation {
	return c.connector.Describe(err)
}
