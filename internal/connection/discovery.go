package connection

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"unicode"
)

// Server is a public discovery candidate. It contains no credentials.
// ID identifies the server independently of its current network address.
type Server struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
}

// Validate rejects incomplete identities, excessive text, and unsafe addresses.
// Adapters must validate network responses before offering them to the user.
func (s Server) Validate() error {
	if s.ID == "" || len(s.ID) > 256 || s.Name == "" || len(s.Name) > 256 || len(s.URL) > 2048 {
		return errors.New("invalid discovered server")
	}
	for _, v := range []string{s.ID, s.Name, s.URL} {
		if strings.IndexFunc(v, unicode.IsControl) >= 0 {
			return errors.New("invalid discovered server text")
		}
	}
	u, err := url.Parse(s.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" {
		return errors.New("invalid discovered server address")
	}
	return nil
}

// Discoverer finds a bounded snapshot of available servers. Implementations own
// their protocol and timeout, honor cancellation, and return validated entries.
// An empty result means that no servers answered, not that sign-in failed.
type Discoverer interface {
	Discover(context.Context) ([]Server, error)
}
