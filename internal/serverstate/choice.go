package serverstate

import (
	"encoding/json"
	"errors"
	"io"
	"os"
)

// Choice remembers the last successful connection for one configuration snapshot.
// Configuration is a digest, not configuration content or credentials.
type Choice struct{ ID, Configuration string }

// LoadChoice reads bounded private selection state. Invalid files stay untouched.
func LoadChoice(path string) (Choice, error) {
	var value Choice
	f, err := os.Open(path)
	if err != nil {
		return value, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return value, err
	}
	if !info.Mode().IsRegular() {
		return value, errors.New("connection choice is not a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(f, 1025))
	if err != nil {
		return value, err
	}
	if len(data) > 1024 || json.Unmarshal(data, &value) != nil || value.ID == "" || len(value.ID) > 64 || len(value.Configuration) != 64 {
		return value, errors.New("invalid connection choice")
	}
	return value, nil
}

// SaveChoice atomically remembers a successful connection without editing settings.
func SaveChoice(path string, value Choice) error {
	if value.ID == "" || len(value.ID) > 64 || len(value.Configuration) != 64 {
		return errors.New("invalid connection choice")
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return WriteFile(path, append(data, '\n'))
}
