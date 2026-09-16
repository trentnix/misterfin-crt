package settings

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Migrate combines existing legacy files into settings.json beside them. It
// preserves relative asset paths and originals, refuses to overwrite a file,
// and rejects malformed legacy JSON. Navigation sounds move into ui, and music
// visual settings use their current names. Domain validation still runs at startup.
func (f *File) Migrate() error { return f.migrate(nil, false) }

// MigrateServer adds validated connection settings and preserves legacy DEBUGLOG
// unless diagnostics.enabled is explicit. Existing settings get a private,
// byte-for-byte .before-server backup before atomic replacement. Existing server
// sections, backups, and files changed since Load are never overwritten.
func (f *File) MigrateServer(server Server, debug bool) error {
	return f.migrate(&server, debug)
}

func (f *File) migrate(server *Server, debug bool) error {
	if !f.legacy && server == nil {
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
			data[name] = s.Data
		}
	}
	if server != nil {
		if data["server"] != nil {
			return errors.New("server settings already exist")
		}
		encoded, err := json.Marshal(server)
		if err != nil {
			return errors.New("cannot encode server settings")
		}
		if len(encoded) > sections["server"] {
			return errors.New("cannot migrate: server section exceeds 4096 bytes")
		}
		if _, err := ParseServer(Section{Data: encoded}); err != nil {
			return err
		}
		data["server"] = encoded
		if debug {
			diagnostics := map[string]json.RawMessage{}
			if raw := data["diagnostics"]; raw != nil {
				if err := json.Unmarshal(raw, &diagnostics); err != nil {
					return errors.New("invalid diagnostics settings")
				}
			}
			if diagnostics["enabled"] == nil {
				diagnostics["enabled"] = json.RawMessage("true")
			}
			data["diagnostics"], err = json.Marshal(diagnostics)
			if err != nil {
				return errors.New("cannot encode diagnostics settings")
			}
		}
	}
	if err := normalizeMigration(data); err != nil {
		return err
	}

	// Added fields must not turn a previously valid section into a runtime fallback.
	for name, raw := range data {
		var compact bytes.Buffer
		if err := json.Compact(&compact, raw); err != nil {
			return errors.New("cannot encode migrated settings")
		}
		if compact.Len() > sections[name] {
			return fmt.Errorf("cannot migrate: %s section exceeds %d bytes", name, sections[name])
		}
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
	encoded = append(encoded, '\n')
	if !f.legacy {
		return f.replaceWithBackup(encoded)
	}
	return writeNewSettings(f.Path, encoded)
}

// writeNewSettings creates a private file without replacing an existing path.
func writeNewSettings(path string, data []byte) error {
	out, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, err = out.Write(data)
	if err == nil {
		err = out.Sync()
	}
	err = errors.Join(err, out.Close())
	if err != nil {
		_ = os.Remove(path)
	}
	return err
}

// unchanged rejects symlinks and concurrent edits before migrating a document.
func (f *File) unchanged() error {
	info, err := os.Lstat(f.Path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("settings must be a regular file for migration")
	}
	current := Read(f.Path, maxFileBytes, true)
	if current.Err != nil {
		return current.Err
	}
	if !bytes.Equal(current.Data, f.original) {
		return errors.New("settings changed since loading; restart migration")
	}
	return nil
}

func (f *File) replaceWithBackup(data []byte) error {
	if err := f.unchanged(); err != nil {
		return err
	}
	if err := writeNewSettings(f.Path+".before-server", f.original); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(f.Path), ".settings-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	_, err = temp.Write(data)
	if err == nil {
		err = temp.Sync()
	}
	err = errors.Join(err, temp.Close())
	if err != nil {
		return err
	}
	if err := f.unchanged(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), f.Path)
}
