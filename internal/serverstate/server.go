package serverstate

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"

	"mistervision/internal/connection"
)

// LoadServer reads a remembered discovery selection. Missing files return
// os.ErrNotExist. Invalid or unreadable files are preserved and return an error.
func LoadServer(path string) (connection.Server, error) {
	var server connection.Server
	f, err := os.Open(path)
	if err != nil {
		return server, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return server, err
	}
	if !info.Mode().IsRegular() {
		return server, errors.New("saved server is not a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(f, 16385))
	if err != nil {
		return server, err
	}
	if len(data) > 16384 || json.Unmarshal(data, &server) != nil {
		return server, errors.New("invalid saved server")
	}
	return server, server.Validate()
}

// SaveServer atomically remembers a validated selection without modifying
// configuration or credentials. The caller must serialize reads and writes.
func SaveServer(path string, server connection.Server) error {
	if err := server.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".server-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	err = json.NewEncoder(f).Encode(server)
	if err == nil {
		err = f.Sync()
	}
	if e := f.Close(); err == nil {
		err = e
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
