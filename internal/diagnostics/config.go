// Package diagnostics provides optional, bounded, asynchronous event logs.
// Callers record only approved metadata, never credentials, raw errors, media
// URLs, response bodies, or decoder output.
package diagnostics

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
)

// Config controls one current log and one rotated log. MaxBytes applies to each.
// Relative paths are resolved beside the configuration file by LoadConfig.
type Config struct {
	Enabled  bool   `json:"enabled"`
	Path     string `json:"path"`
	MaxBytes int64  `json:"max_bytes"`
}

// LoadConfig reads diagnostics.json. A missing file uses the legacy DEBUGLOG
// switch, debug.log beside the configuration, and 1 MiB per file. An explicit
// enabled value overrides DEBUGLOG. Invalid settings are reported before opening
// the log. No directories or log files are created here.
func LoadConfig(path string, legacy bool) (Config, error) {
	c := Config{Enabled: legacy, Path: "debug.log", MaxBytes: 1 << 20}
	f, err := os.Open(path)
	if err != nil && !os.IsNotExist(err) {
		return c, err
	}
	if err == nil {
		defer f.Close()
		data, err := io.ReadAll(io.LimitReader(f, 4097))
		if err != nil {
			return c, err
		}
		if len(data) > 4096 {
			return c, errors.New("diagnostics configuration exceeds 4 KiB")
		}
		data = bytes.TrimSpace(data)
		if len(data) == 0 || data[0] != '{' {
			return c, errors.New("diagnostics configuration must be an object")
		}
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&c); err != nil {
			return c, errors.New("invalid diagnostics configuration")
		}
		var extra any
		if dec.Decode(&extra) != io.EOF {
			return c, errors.New("diagnostics configuration must contain one object")
		}
	}
	if c.Path == "" || c.MaxBytes < 4096 || c.MaxBytes > 64<<20 {
		return c, errors.New("diagnostics requires a path and max_bytes between 4096 and 67108864")
	}
	if !filepath.IsAbs(c.Path) {
		c.Path = filepath.Join(filepath.Dir(path), c.Path)
	}
	return c, nil
}
