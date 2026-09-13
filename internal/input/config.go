package input

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"misterfin-go/internal/input/evdev"
)

// LoadConfig reads an optional input.json beside the Jellyfin configuration.
// An explicit path must exist. Malformed files always fail with their path.
func LoadConfig(path, jellyfinPath string) (evdev.Config, error) {
	var config evdev.Config
	explicit := path != ""
	if !explicit {
		path = filepath.Join(filepath.Dir(jellyfinPath), "input.json")
	}
	f, err := os.Open(path)
	if !explicit && errors.Is(err, os.ErrNotExist) {
		return config, nil
	}
	if err != nil {
		return config, err
	}
	defer f.Close()
	decoder := json.NewDecoder(f)
	decoder.DisallowUnknownFields()
	decoded := &config
	if err = decoder.Decode(&decoded); err == nil && decoded == nil {
		err = errors.New("expected a JSON object")
	}
	if err == nil {
		var extra any
		if err = decoder.Decode(&extra); errors.Is(err, io.EOF) {
			err = config.Validate()
		} else if err == nil {
			err = errors.New("expected a single JSON object")
		}
	}
	if err != nil {
		return config, fmt.Errorf("input configuration %s: %w", path, err)
	}
	return config, nil
}
