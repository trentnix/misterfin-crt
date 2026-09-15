package main

import (
	"path/filepath"

	"misterfin-crt/internal/settings"
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
