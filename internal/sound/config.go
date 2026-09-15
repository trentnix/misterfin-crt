// Package sound provides optional UI feedback independently of audio devices.
package sound

import (
	"fmt"

	"misterfin-crt/internal/settings"
)

// Config controls navigation sounds. Volume scales the original PCM amplitude.
type Config struct {
	Enabled bool `json:"enabled"`
	Volume  int  `json:"volume"`
}

// Defaults enables subdued feedback, about 14 dB below the C client's clip gain.
func Defaults() Config { return Config{Enabled: true, Volume: 10} }

// LoadConfig reads a legacy sound settings file. Missing files use Defaults.
func LoadConfig(path string) (Config, error) { return ParseConfig(settings.Read(path, 4096, false)) }

// ParseConfig validates ui.navigation_sounds over subdued default feedback.
// Zero volume is silent. Invalid values are returned for caller-controlled recovery.
func ParseConfig(source settings.Section) (Config, error) {
	cfg := Defaults()
	if err := source.Decode(&cfg); err != nil {
		return cfg, err
	}
	if cfg.Volume < 0 || cfg.Volume > 100 {
		return cfg, fmt.Errorf("sound volume must be between 0 and 100")
	}
	return cfg, nil
}
