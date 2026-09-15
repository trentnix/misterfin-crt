// Package settings reads the sectioned application settings without depending
// on any display, player, or configuration consumer. Consumers validate values.
package settings

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const maxFileBytes = 256 << 10

var sections = map[string]int{"ui": 4096, "background": 4096, "display": 4096, "sounds": 4096, "diagnostics": 4096, "input": 64 << 10, "music_visuals": 64 << 10}

// Section is an immutable startup snapshot. Path locates relative assets. Nil
// Data selects defaults. Err is deferred so an invalid optional section can
// recover independently of valid sections. Consumers must not mutate Data.
type Section struct {
	Path string
	Data []byte
	Err  error
}

// Read reads a legacy settings file with a bounded allocation. An absent file
// selects defaults unless required is true, as for an explicit path override.
func Read(path string, limit int, required bool) Section {
	s := Section{Path: path}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) && !required {
		return s
	}
	if err != nil {
		s.Err = err
		return s
	}
	defer f.Close()
	s.Data, s.Err = io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if len(s.Data) > limit {
		s.Err = fmt.Errorf("settings exceed %d bytes", limit)
	}
	return s
}

// Decode merges a JSON object into caller-supplied defaults. It rejects unknown
// keys and trailing content. File and section failures never expose raw values
// in diagnostics unless a caller explicitly logs an error, which it must not do.
func (s Section) Decode(dst any) error {
	if s.Err != nil {
		return s.Err
	}
	if s.Data == nil {
		return nil
	}
	data := bytes.TrimSpace(s.Data)
	if len(data) == 0 || data[0] != '{' {
		return errors.New("settings must be a JSON object")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if dec.Decode(new(any)) != io.EOF {
		return errors.New("settings must contain one JSON object")
	}
	return nil
}

// File is one immutable settings.json snapshot. If the optional file is absent,
// Section reads the corresponding legacy file. A present file never merges
// omitted sections with legacy files.
type File struct {
	Path   string
	data   map[string]json.RawMessage
	legacy bool
}

// Load reads settings.json once. An absent optional file enables legacy
// fallback. Malformed JSON and unknown section names are errors because display
// and input intent cannot be recovered reliably from a broken document.
func Load(path string, required bool) (*File, error) {
	s := Read(path, maxFileBytes, required)
	f := &File{Path: path, legacy: s.Data == nil && s.Err == nil}
	if err := s.Decode(&f.data); err != nil {
		return nil, fmt.Errorf("settings %s: %w", path, err)
	}
	renameKey(f.data, "music", "music_visuals")
	for name := range f.data {
		if _, ok := sections[name]; !ok {
			return nil, fmt.Errorf("settings %s: unknown section %q", path, name)
		}
	}
	return f, nil
}

// Section returns one section or its defaults. Legacy files are read only when
// no settings.json exists. Errors stay local until the owning consumer decides
// whether to recover or stop startup. ui.navigation_sounds selects the nested
// object first, then the legacy sounds section or file when the object is absent.
func (f *File) Section(name string) Section {
	if name == "ui.navigation_sounds" {
		nested := f.Section("ui").child("navigation_sounds")
		if nested.Data != nil || nested.Err != nil {
			return nested
		}
		return f.Section("sounds")
	}
	if name == "music" {
		name = "music_visuals"
	}
	limit, ok := sections[name]
	if !ok {
		return Section{Path: f.Path, Err: errors.New("unknown settings section")}
	}
	if f.legacy {
		if name == "music_visuals" {
			name = "music"
		}
		return Read(filepath.Join(filepath.Dir(f.Path), name+".json"), limit, false)
	}
	s := Section{Path: f.Path, Data: f.data[name]}
	if len(s.Data) > limit {
		// Formatting added by migration must not invalidate a previously valid
		// section. The document limit already bounds the raw allocation.
		var compact bytes.Buffer
		if err := json.Compact(&compact, s.Data); err != nil || compact.Len() > limit {
			s.Err = fmt.Errorf("%s section exceeds %d bytes", name, limit)
		}
	}
	return s
}

// Migrate combines existing legacy files into settings.json beside them. It
// preserves relative asset paths and originals, refuses to overwrite a file,
// and rejects malformed legacy JSON. Navigation sounds move into ui, and music
// visual settings use their current names. Domain validation still runs at startup.
func (f *File) Migrate() error {
	if !f.legacy {
		return errors.New("settings file already exists")
	}
	data := map[string]json.RawMessage{}
	for name := range sections {
		s := f.Section(name)
		var object map[string]json.RawMessage
		if err := s.Decode(&object); err != nil {
			return fmt.Errorf("cannot migrate %s: %w", s.Path, err)
		}
		if s.Data != nil {
			if name == "music_visuals" {
				renameKey(object, "default", "default_background")
				renameKey(object, "meters", "show_audio_meters")
				encoded, err := json.Marshal(object)
				if err != nil {
					return err
				}
				if len(encoded) > sections[name] {
					return fmt.Errorf("cannot migrate %s: renamed section exceeds %d bytes", s.Path, sections[name])
				}
				data[name] = encoded
			} else {
				data[name] = s.Data
			}
		}
	}
	if sounds, ok := data["sounds"]; ok {
		ui := map[string]json.RawMessage{}
		if raw, exists := data["ui"]; exists {
			if err := json.Unmarshal(raw, &ui); err != nil {
				return err
			}
		}
		if _, exists := ui["navigation_sounds"]; !exists {
			ui["navigation_sounds"] = sounds
		}
		encoded, err := json.Marshal(ui)
		if err != nil {
			return err
		}
		if len(encoded) > sections["ui"] {
			return errors.New("cannot migrate: combined ui section exceeds 4096 bytes")
		}
		data["ui"] = encoded
		delete(data, "sounds")
	}
	encoded, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	if len(encoded)+1 > maxFileBytes {
		// Compact large migrations so readable indentation cannot exceed the
		// document budget. Section limits bound the compact result below it.
		encoded, err = json.Marshal(data)
		if err != nil {
			return err
		}
	}
	// Exclusive creation protects existing settings. Failed writes remove only
	// the new file created here. The original legacy files are never modified.
	out, err := os.OpenFile(f.Path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, err = out.Write(append(encoded, '\n'))
	if err == nil {
		err = out.Sync()
	}
	err = errors.Join(err, out.Close())
	if err != nil {
		_ = os.Remove(f.Path)
	}
	return err
}

// renameKey accepts an old spelling without overriding an explicitly supplied
// replacement, including null. Migration writes only the current spelling.
func renameKey(object map[string]json.RawMessage, old, current string) {
	if value, ok := object[old]; ok {
		if _, exists := object[current]; !exists {
			object[current] = value
		}
		delete(object, old)
	}
}

// child extracts an optional nested object without validating sibling values.
// An invalid parent propagates its error. Explicit null remains distinct from
// omission so an invalid override cannot enable sounds through legacy defaults.
func (s Section) child(name string) Section {
	var object map[string]json.RawMessage
	err := s.Decode(&object)
	return Section{Path: s.Path, Data: object[name], Err: err}
}
