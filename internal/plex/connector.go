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
	if err := ctx.Err(); err != nil {
		return connection.Session{}, err
	}
	if interaction.ProfileAction == connection.ProfileForget {
		return connection.Session{}, c.signOut(ctx, interaction)
	}
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
	p := connection.Presentation{Kind: connection.SetupFailure, Title: titleConnectFailed, Retry: "Retry",
		Message: messageConnectFailed}
	if c.Config.Server == "" {
		p.Message = messageDiscoveredConnectFailed
	}
	switch {
	case errors.Is(err, connection.ErrSignedOut):
		p.Title, p.Message = connection.SignOutIncompleteTitle, connection.SignOutIncompleteMessage
		p.Path, p.PathLabel = StateDir(c.StateDir), "Sign-in folder"
	case errors.Is(err, connection.ErrCanceled):
		p.Title, p.Message = connection.SignInCanceledTitle, connection.SignInCanceledMessage
	case errors.Is(err, ErrSessionSave):
		p.Title, p.Message = connection.SignInStorageTitle, connection.SignInStorageMessage
		p.Path, p.PathLabel = StateDir(c.StateDir), "Sign-in folder"
		if absolute, e := filepath.Abs(p.Path); e == nil {
			p.Path = absolute
		}
	case errors.Is(err, errHome):
		p.Title, p.Message = titleProfileFailed, messageProfileFailed
	case errors.Is(err, ErrCodeExpired):
		p.Title, p.Message, p.Retry = connection.CodeExpiredTitle, messageCodeExpired, "New code"
	case errors.Is(err, media.ErrUnauthorized):
		p.Title, p.Message, p.Retry = connection.SignInRequiredTitle, messageSignInRejected, "Sign in"
	case errors.Is(err, errRecovery):
		p.Title, p.Message = titleRecoveryFailed, messageRecoveryFailed
	case errors.Is(err, errNoServers):
		p.Title, p.Message = titleNoServers, messageNoServers
	case errors.Is(err, errServersUnreachable):
		p.Title, p.Message = titleServersUnavailable, messageServersUnavailable
	case errors.Is(err, errDiscovery):
		p.Title, p.Message = titleDiscoveryFailed, messageDiscoveryFailed
	case errors.Is(err, errServerURL):
		p.Title, p.Message = connection.ConfigurationTitle, messageConfigInvalid
	}
	return p
}
