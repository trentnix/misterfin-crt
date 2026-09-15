package browser

import (
	"encoding/json"
	"strings"
	"unicode"

	"misterfin-crt/internal/settings"
)

// LoadTitle reads a legacy optional ui.json. Nil selects the default heading.
func LoadTitle(path string) (*string, error) { return ParseTitle(settings.Read(path, 4096, false)) }

// ParseTitle reads the ui section. Omitted titles select the default heading.
// Explicit empty values hide it. Whitespace and controls are normalized once.
func ParseTitle(source settings.Section) (*string, error) {
	var config struct {
		Title            *string         `json:"title"`
		NavigationSounds json.RawMessage `json:"navigation_sounds"` // Validated independently by the sound loader.
	}
	if err := source.Decode(&config); err != nil {
		return nil, err
	}
	if config.Title == nil {
		return nil, nil
	}
	title := strings.Join(strings.Fields(*config.Title), " ")
	title = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, title)
	title = strings.TrimSpace(title)
	return &title, nil
}
