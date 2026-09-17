package settings

import (
	"bytes"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"unicode"
)

// Connection names one independent server account. ID stays stable when its
// display name changes. Server uses the same validated schema as server.
type Connection struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Server Server `json:"server"`
}

var connectionID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,47}$`)

// ParseConnections validates up to 16 named connections. Missing connections
// preserve single-server setup. Invalid entries never silently select a server.
func ParseConnections(section Section) ([]Connection, error) {
	if section.Data == nil && section.Err == nil {
		return nil, nil
	}
	var document struct {
		Profiles []struct {
			ID     string          `json:"id"`
			Name   string          `json:"name"`
			Server json.RawMessage `json:"server"`
		} `json:"profiles"`
	}
	if err := section.Decode(&document); err != nil || len(document.Profiles) > 16 {
		return nil, errors.New("connections.profiles must be an array of at most 16 entries")
	}
	entries := document.Profiles
	seen := map[string]bool{}
	out := make([]Connection, 0, len(entries))
	for _, entry := range entries {
		if !connectionID.MatchString(entry.ID) || seen[entry.ID] {
			return nil, errors.New("connections require unique IDs using letters, numbers, underscores, or hyphens")
		}
		if strings.TrimSpace(entry.Name) == "" || len(entry.Name) > 128 || strings.IndexFunc(entry.Name, unicode.IsControl) >= 0 {
			return nil, errors.New("each connection requires a short name without control characters")
		}
		var compact bytes.Buffer
		if json.Compact(&compact, entry.Server) != nil || compact.Len() > 4096 {
			return nil, errors.New("each connection requires server settings within 4096 bytes")
		}
		server, err := ParseServer(Section{Data: entry.Server, Path: section.Path})
		if err != nil || server == nil {
			return nil, errors.New("each connection requires valid server settings")
		}
		seen[entry.ID] = true
		out = append(out, Connection{ID: entry.ID, Name: entry.Name, Server: *server})
	}
	return out, nil
}
