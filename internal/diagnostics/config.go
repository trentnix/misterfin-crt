// Package diagnostics provides optional, bounded, asynchronous event logs.
// Callers record only approved metadata, never credentials, raw errors, media
// URLs, response bodies, or decoder output.
package diagnostics

import (
	"errors"
	"path/filepath"

	"mistervision/internal/settings"
)

// Config controls one current log and one rotated log. MaxBytes applies to each.
// Relative paths are resolved beside the configuration file by LoadConfig.
type Config struct {
	Enabled  bool   `json:"enabled"`
	Path     string `json:"path"`
	MaxBytes int64  `json:"max_bytes"`
}

// LoadConfig reads legacy diagnostics settings and resolves their log path.
func LoadConfig(path string, legacy bool) (Config, error) {
	return ParseConfig(settings.Read(path, 4096, false), legacy)
}

// ParseConfig applies the diagnostics section over DEBUGLOG-compatible defaults.
// Relative log paths resolve beside the settings file. No files are opened here.
func ParseConfig(source settings.Section, legacy bool) (Config, error) {
	c := Config{Enabled: legacy, Path: "debug.log", MaxBytes: 1 << 20}
	if err := source.Decode(&c); err != nil {
		return c, err
	}
	if c.Path == "" || c.MaxBytes < 4096 || c.MaxBytes > 64<<20 {
		return c, errors.New("diagnostics requires a path and max_bytes between 4096 and 67108864")
	}
	if !filepath.IsAbs(c.Path) {
		c.Path = filepath.Join(filepath.Dir(source.Path), c.Path)
	}
	return c, nil
}
