// Package settings owns section reading, UI and server schemas, and legacy compatibility.
// Domain consumers validate their own values. Migration uses the same aliases.
package settings

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
)

const maxFileBytes = 256 << 10

var sections = map[string]int{"server": 4096, "ui": 4096, "background": 4096, "display": 4096, "sounds": 4096, "diagnostics": 4096, "input": 64 << 10, "music_visuals": 64 << 10}

// File holds immutable startup snapshots. UI fields are decoded once. Relative
// asset paths stay attached to their source, including legacy files.
type File struct {
	Path     string
	sources  map[string]Section
	legacy   bool
	original []byte // Original document protects explicit migration from stale writes.
	ui       UI
}

// Load reads the shared document or snapshots legacy files when the default is
// absent. Root errors stop loading. Section errors remain local to consumers.
func Load(path string, required bool) (*File, error) {
	source := Read(path, maxFileBytes, required)
	var data map[string]json.RawMessage
	if err := source.Decode(&data); err != nil {
		return nil, fmt.Errorf("settings %s: %w", path, err)
	}
	renameKey(data, "music", "music_visuals")
	for name := range data {
		if _, ok := sections[name]; !ok {
			return nil, fmt.Errorf("settings %s: unknown section %q", path, name)
		}
	}
	f := &File{Path: path, original: source.Data, legacy: source.Data == nil, sources: make(map[string]Section)}
	for name, limit := range sections {
		s := Section{Path: path, Data: data[name]}
		if f.legacy && name != "server" {
			file := name
			if file == "music_visuals" {
				file = "music"
			}
			s = Read(filepath.Join(filepath.Dir(path), file+".json"), limit, false)
		} else if len(s.Data) > limit {
			// Formatting must not invalidate otherwise bounded settings.
			var compact bytes.Buffer
			if err := json.Compact(&compact, s.Data); err != nil || compact.Len() > limit {
				s.Err = fmt.Errorf("%s section exceeds %d bytes", name, limit)
			}
		}
		f.sources[name] = s
	}
	f.ui = ParseUI(f.sources["ui"])
	f.ui.NavigationSounds = preferSection(f.ui.NavigationSounds, f.sources["sounds"])
	return f, nil
}

// Section returns a startup snapshot. Old section names remain accepted.
// ui.navigation_sounds uses the nested object before the legacy sounds source.
func (f *File) Section(name string) Section {
	if name == "ui.navigation_sounds" {
		return f.ui.NavigationSounds
	}
	if name == "music" {
		name = "music_visuals"
	}
	if s, ok := f.sources[name]; ok {
		return s
	}
	return Section{Path: f.Path, Err: errors.New("unknown settings section")}
}

// UI returns decoded heading and carousel options and a separate sound source.
// Callers must not mutate borrowed setting values or sound data.
func (f *File) UI() UI { return f.ui }
