package main

import (
	"misterfin-crt/internal/connection"
	"misterfin-crt/internal/diagnostics"
	"misterfin-crt/internal/jellyfin"
	jfconnection "misterfin-crt/internal/jellyfin/connection"
	"misterfin-crt/internal/plex"
	"misterfin-crt/internal/settings"
)

// serverConnector is the application assembly point for media providers.
// Only an absent server section permits legacy Jellyfin configuration.
func serverConnector(source *settings.File, configPath, stateDir, version string, log *diagnostics.Log) (connection.Connector, error) {
	c, err := settings.ParseServer(source.Section("server"))
	if err != nil {
		return nil, err
	}
	if c == nil {
		return jfconnection.Connector{ConfigPath: configPath, StateDir: stateDir, Version: version, Diagnostics: log}, nil
	}
	switch c.Provider {
	case "plex":
		config := plex.Config{Server: c.URL, InsecureTLS: c.InsecureTLS, MaxWidth: c.Transcode.MaxWidth, MaxHeight: c.Transcode.MaxHeight, VideoBitrate: c.Transcode.VideoBitrate}
		return plex.Connector{Config: config, StateDir: stateDir, Version: version, Diagnostics: log}, nil
	default:
		config := jellyfin.Config{Server: c.URL, InsecureTLS: c.InsecureTLS, Transcode: jellyfin.TranscodeProfile{MaxWidth: c.Transcode.MaxWidth, MaxHeight: c.Transcode.MaxHeight, VideoBitrate: c.Transcode.VideoBitrate}}
		if c.Jellyfin != nil {
			config.APIKey, config.Username = c.Jellyfin.APIKey, c.Jellyfin.Username
		}
		return jfconnection.Connector{Config: &config, ConfigPath: source.Path, StateDir: stateDir, Version: version, Diagnostics: log}, nil
	}
}
