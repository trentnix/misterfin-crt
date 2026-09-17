package main

import (
	"mistervision/internal/connection"
	"mistervision/internal/diagnostics"
	"mistervision/internal/jellyfin"
	jfconnection "mistervision/internal/jellyfin/connection"
	"mistervision/internal/plex"
	"mistervision/internal/settings"
)

// serverConnector is the application assembly point for media providers.
// Only an absent server section permits legacy Jellyfin configuration or discovery.
func serverConnector(source *settings.File, configPath, stateDir, version string, log *diagnostics.Log) (connection.Connector, error) {
	c, err := settings.ParseServer(source.Section("server"))
	if err != nil {
		return nil, err
	}
	if c == nil {
		return jfconnection.Connector{Discovery: jellyfin.Discovery{}, SettingsPath: source.Path, ConfigPath: configPath, StateDir: stateDir, Version: version, Diagnostics: log}, nil
	}
	return configuredConnector(*c, source.Path, stateDir, version, log), nil
}

// configuredConnector assembles one validated, account-scoped backend.
func configuredConnector(c settings.Server, settingsPath, stateDir, version string, log *diagnostics.Log) connection.Connector {
	switch c.Provider {
	case "plex":
		config := plex.Config{Server: c.URL, InsecureTLS: c.InsecureTLS, MaxWidth: c.Transcode.MaxWidth, MaxHeight: c.Transcode.MaxHeight, VideoBitrate: c.Transcode.VideoBitrate}
		return plex.Connector{Config: config, StateDir: stateDir, Version: version, Diagnostics: log}
	default:
		config := jellyfin.Config{Server: c.URL, InsecureTLS: c.InsecureTLS, Transcode: jellyfin.TranscodeProfile{MaxWidth: c.Transcode.MaxWidth, MaxHeight: c.Transcode.MaxHeight, VideoBitrate: c.Transcode.VideoBitrate}}
		if c.Jellyfin != nil {
			config.APIKey, config.Username = c.Jellyfin.APIKey, c.Jellyfin.Username
		}
		return jfconnection.Connector{Config: &config, ConfigPath: settingsPath, StateDir: stateDir, Version: version, Diagnostics: log}
	}
}
