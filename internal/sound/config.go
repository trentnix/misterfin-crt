// Package sound provides optional UI feedback independently of audio devices.
package sound

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// Config controls navigation sounds. Volume scales the original PCM amplitude.
type Config struct {
	Enabled bool `json:"enabled"`
	Volume  int  `json:"volume"`
}

// Defaults enables subdued feedback, about 14 dB below the C client's clip gain.
func Defaults() Config { return Config{Enabled: true, Volume: 10} }

// LoadConfig reads one JSON object. A missing file uses Defaults. Unknown keys,
// oversized files, and volumes outside 0–100 are errors. Zero volume is silent.
func LoadConfig(path string) (Config, error) {
	cfg := Defaults()
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 4097))
	if err != nil {
		return cfg, err
	}
	if len(data) > 4096 {
		return cfg, fmt.Errorf("sound configuration exceeds 4 KiB")
	}
	if len(bytes.TrimSpace(data)) == 0 || bytes.TrimSpace(data)[0] != '{' {
		return cfg, fmt.Errorf("sound configuration must be a JSON object")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err = dec.Decode(&cfg); err != nil {
		return cfg, fmt.Errorf("sound configuration: %w", err)
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		return cfg, fmt.Errorf("sound configuration must contain one JSON object")
	}
	if cfg.Volume < 0 || cfg.Volume > 100 {
		return cfg, fmt.Errorf("sound volume must be between 0 and 100")
	}
	return cfg, nil
}
