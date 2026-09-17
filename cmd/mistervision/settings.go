package main

import (
	"errors"
	"os"
	"path/filepath"

	"mistervision/internal/jellyfin"
	"mistervision/internal/settings"
)

// loadSettings resolves the one application settings file. Missing default
// settings enable legacy-file compatibility. Explicit overrides must exist,
// except when creating that file with the migration command.
func loadSettings(o launchOptions) (*settings.File, error) {
	path := o.settingsPath
	if path == "" {
		path = filepath.Join(filepath.Dir(o.config), "settings.json")
	}
	return settings.Load(path, o.settingsPath != "" && !o.migrateSettings)
}

// migrateSettings consolidates legacy connection and application settings without
// opening a display or contacting a server. Existing sign-in files stay untouched.
func migrateSettings(o launchOptions, source *settings.File) error {
	if section := source.Section("server"); section.Data != nil || section.Err != nil {
		return errors.New("server settings already exist; migration is not needed")
	}
	legacy, err := jellyfin.LoadConfig(o.config)
	if errors.Is(err, os.ErrNotExist) {
		return source.Migrate()
	}
	if err != nil {
		return err
	}
	server := settings.Server{Provider: "jellyfin", URL: legacy.Server, InsecureTLS: legacy.InsecureTLS, Transcode: settings.Transcode{
		MaxWidth: legacy.Transcode.MaxWidth, MaxHeight: legacy.Transcode.MaxHeight, VideoBitrate: legacy.Transcode.VideoBitrate}}
	if legacy.APIKey != "" || legacy.Username != "" {
		server.Jellyfin = &settings.JellyfinLogin{APIKey: legacy.APIKey, Username: legacy.Username}
	}
	return source.MigrateServer(server, legacy.DebugLog)
}
