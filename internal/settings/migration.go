package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

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
			data[name] = s.Data
		}
	}
	if err := normalizeMigration(data); err != nil {
		return err
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
