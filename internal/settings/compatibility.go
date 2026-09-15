package settings

import (
	"encoding/json"
	"fmt"
)

// MusicVisuals normalizes legacy field names without applying domain defaults.
// Parsing and migration share this path. Explicit current fields, including
// null and false, take precedence. The input snapshot is never modified.
func MusicVisuals(source Section) Section {
	if source.Data == nil || source.Err != nil {
		return source
	}
	var object map[string]json.RawMessage
	if err := source.Decode(&object); err != nil {
		return Section{Path: source.Path, Err: err}
	}
	// Preserve validation of malformed aliases even when a current field wins.
	var legacy struct {
		Default *string `json:"default"`
		Meters  *bool   `json:"meters"`
	}
	if err := json.Unmarshal(source.Data, &legacy); err != nil {
		return Section{Path: source.Path, Err: err}
	}
	renameKey(object, "default", "default_background")
	renameKey(object, "meters", "show_audio_meters")
	data, err := json.Marshal(object)
	return Section{Path: source.Path, Data: data, Err: err}
}

// preferSection keeps an explicit nested value or error. Only omission permits
// the legacy source to supply values, preserving explicit null and empty objects.
func preferSection(current, legacy Section) Section {
	if current.Data != nil || current.Err != nil {
		return current
	}
	return legacy
}

// normalizeMigration writes the current schema using the runtime precedence
// rules. Oversized merged UI objects fail before any destination is created.
func normalizeMigration(data map[string]json.RawMessage) error {
	if raw, ok := data["music_visuals"]; ok {
		source := MusicVisuals(Section{Data: raw})
		if source.Err != nil {
			return source.Err
		}
		if len(source.Data) > sections["music_visuals"] {
			return fmt.Errorf("cannot migrate: music_visuals section exceeds %d bytes", sections["music_visuals"])
		}
		data["music_visuals"] = source.Data
	}
	if sounds, ok := data["sounds"]; ok {
		ui := map[string]json.RawMessage{}
		if raw, exists := data["ui"]; exists {
			if err := json.Unmarshal(raw, &ui); err != nil {
				return err
			}
		}
		selected := preferSection(Section{Data: ui["navigation_sounds"]}, Section{Data: sounds})
		ui["navigation_sounds"] = selected.Data
		encoded, err := json.Marshal(ui)
		if err != nil {
			return err
		}
		if len(encoded) > sections["ui"] {
			return fmt.Errorf("cannot migrate: combined ui section exceeds %d bytes", sections["ui"])
		}
		data["ui"] = encoded
		delete(data, "sounds")
	}
	return nil
}

// renameKey accepts an alias without replacing an explicitly supplied value.
func renameKey(object map[string]json.RawMessage, old, current string) {
	if value, ok := object[old]; ok {
		if _, exists := object[current]; !exists {
			object[current] = value
		}
		delete(object, old)
	}
}
