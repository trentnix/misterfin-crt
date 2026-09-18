package serverstate

import (
	"os"
	"path/filepath"
)

// WriteFile atomically replaces private state with data in a mode-0600 file.
// It syncs and closes the temporary file before renaming it. Errors leave the
// previous destination intact and remove the temporary file. The caller owns
// encoding, validation, size limits, and serialization of writes to path.
func WriteFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".state-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
