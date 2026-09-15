package settings

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
)

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
