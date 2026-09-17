package plex

import (
	"context"
	"errors"
	"net/url"
	"path/filepath"
	"strings"

	"mistervision/internal/connection"
	"mistervision/internal/diagnostics"
	"mistervision/internal/media"
	"mistervision/internal/serverstate"
)

// Connector signs in to one explicitly configured Plex server. Calls sharing
// StateDir must be serialized. Plex credentials live in StateDir/plex.
type Connector struct {
	Config            Config
	StateDir, Version string
	Diagnostics       *diagnostics.Log
}

var _ connection.Connector = Connector{}

// Connect validates saved credentials or requests approval at plex.tv/link.
// A connected session has no remote-control source in this initial adapter.
func (c Connector) Connect(ctx context.Context, progress func(connection.Presentation)) (connection.Session, error) {
	server, err := serverURL(c.Config.Server)
	if err != nil {
		return connection.Session{}, err
	}
	dir := StateDir(c.StateDir)
	saved, recovered, err := serverstate.LoadSession(dir, server)
	if err != nil {
		return connection.Session{}, ErrSessionSave
	}
	config := c.Config
	config.Server = server
	client := NewClient(config, saved)
	client.Version, client.Diagnostics = c.Version, c.Diagnostics
	if recovered {
		c.Diagnostics.Record("authentication.session-recovered")
	}
	err = client.Authenticate(ctx, dir, func(code string) {
		if progress != nil {
			progress(connection.Presentation{Kind: connection.SetupApproval, Title: "Link Plex", Code: code, Recovered: recovered, Retry: "New code",
				Message: "Open plex.tv/link in a signed-in browser.\nEnter this code to approve MiSTerVision."})
		}
	})
	if err != nil {
		return connection.Session{}, err
	}
	return connection.Session{Server: client, Recovered: recovered}, nil
}

var errServerURL = errors.New("Plex requires an HTTP or HTTPS server address without credentials, query, or fragment")

func serverURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" {
		return "", errServerURL
	}
	return strings.TrimRight(u.String(), "/"), nil
}

// Describe returns public recovery instructions without server response text.
func (c Connector) Describe(err error) connection.Presentation {
	if err == nil {
		return connection.Presentation{Kind: connection.SetupConnecting, Title: "Connecting to Plex", Message: "Checking your connection and saved sign-in."}
	}
	p := connection.Presentation{Kind: connection.SetupFailure, Title: "Can't connect to Plex", Retry: "Retry",
		Message: "Check the server address and network connection.\nMake sure Plex Media Server is running, then retry."}
	switch {
	case errors.Is(err, errServerURL):
		p.Title, p.Message = "Check your configuration", "Set server.url to your Plex server's HTTP or HTTPS address.\nFor example: http://192.168.1.10:32400"
	case errors.Is(err, ErrSessionSave):
		p.Title, p.Message = "Can't save or read sign-in", "Make sure this folder is writable, then retry.\nYour saved sign-in has not been cleared."
		p.Path, p.PathLabel = StateDir(c.StateDir), "Sign-in folder"
		if absolute, e := filepath.Abs(p.Path); e == nil {
			p.Path = absolute
		}
	case errors.Is(err, ErrCodeExpired):
		p.Title, p.Message, p.Retry = "Code expired", "Request a new code, then approve it at plex.tv/link.", "New code"
	case errors.Is(err, media.ErrUnauthorized):
		p.Title, p.Message, p.Retry = "Sign-in required", "Plex rejected your sign-in. Link your account again.\nThe account must have access to this server.", "Sign in"
	}
	return p
}
