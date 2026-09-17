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

// Connector signs in to a configured Plex server, or links an account and
// offers server selection when Config.Server is empty. Calls sharing StateDir
// must be serialized. Plex credentials live in StateDir/plex.
type Connector struct {
	Config            Config
	StateDir, Version string
	Diagnostics       *diagnostics.Log
}

var _ connection.Connector = Connector{}

// Connect validates saved credentials or requests approval at plex.tv/link.
// Plex sessions do not provide a remote-control source.
func (c Connector) Connect(ctx context.Context, interaction connection.Interaction) (connection.Session, error) {
	if c.Config.Server != "" {
		server, err := serverURL(c.Config.Server)
		if err != nil {
			return connection.Session{}, err
		}
		c.Config.Server = server
	}
	account := NewClient(Config{}, serverstate.Session{})
	account.Version, account.Diagnostics = c.Version, c.Diagnostics
	return c.connectDiscovered(ctx, interaction, &serverDiscovery{account: account, lan: gdmDiscovery{}})
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
	if c.Config.Server == "" {
		p.Message = "Check your server and internet connection, then retry.\nPress Back to choose a server again."
	}
	switch {
	case errors.Is(err, ErrSessionSave):
		p.Title, p.Message = "Can't save or read sign-in", "Make sure this folder is writable, then retry.\nYour saved sign-in has not been cleared."
		p.Path, p.PathLabel = StateDir(c.StateDir), "Sign-in folder"
		if absolute, e := filepath.Abs(p.Path); e == nil {
			p.Path = absolute
		}
	case errors.Is(err, errHome):
		p.Title, p.Message = "Can’t open profile", "Check your internet connection and Plex Home settings, then retry.\nYour previous connection has been kept."
	case errors.Is(err, ErrCodeExpired):
		p.Title, p.Message, p.Retry = "Code expired", "Request a new code, then approve it at plex.tv/link.", "New code"
	case errors.Is(err, media.ErrUnauthorized):
		p.Title, p.Message, p.Retry = "Sign-in required", "Plex rejected your sign-in. Link your account again.\nThe account must have access to this server.", "Sign in"
	case errors.Is(err, errRecovery):
		p.Title, p.Message = "Plex server unavailable", "Check your server and network, then retry.\nYour saved connection and sign-in have been kept.\nPress Back to choose a server."
	case errors.Is(err, errNoServers):
		p.Title, p.Message = "No Plex servers", "This account has no available media servers.\nCheck server sharing and your Plex account, then retry."
	case errors.Is(err, errServersUnreachable):
		p.Title, p.Message = "Plex servers unavailable", "Check that your server is running and its network addresses are correct.\nRetry, or configure its address in settings.json."
	case errors.Is(err, errDiscovery):
		p.Title, p.Message = "Can't find Plex servers", "Check your internet connection and Plex account, then retry.\nYour saved connection has not been cleared."
	case errors.Is(err, errServerURL):
		p.Title, p.Message = "Check your configuration", "Set server.url to your Plex server's HTTP or HTTPS address.\nFor example: http://192.168.1.10:32400"
	}
	return p
}
