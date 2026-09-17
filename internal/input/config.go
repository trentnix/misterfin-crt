package input

import (
	"fmt"
	"path/filepath"

	"mistervision/internal/input/evdev"
	"mistervision/internal/settings"
)

// LoadConfig reads legacy controller settings. An explicit path must exist.
func LoadConfig(path, jellyfinPath string) (evdev.Config, error) {
	required := path != ""
	if !required {
		path = filepath.Join(filepath.Dir(jellyfinPath), "input.json")
	}
	return ParseConfig(settings.Read(path, 64<<10, required))
}

// ParseConfig validates the input section before bindings reach a device.
// Errors include the source path so users can repair hardware mappings.
func ParseConfig(source settings.Section) (evdev.Config, error) {
	var config evdev.Config
	err := source.Decode(&config)
	if err == nil {
		err = config.Validate()
	}
	if err != nil {
		return config, fmt.Errorf("input configuration %s: %w", source.Path, err)
	}
	return config, nil
}
