package connection

import (
	"errors"
	"os"
	"path/filepath"

	"mistervision/internal/connection"
	"mistervision/internal/jellyfin"
)

// connectionStage identifies the operation that failed during sign-in.
type connectionStage uint8

const (
	connectionConfig connectionStage = iota
	connectionSession
	connectionAuthentication
	connectionDiscovery
	connectionServerStorage
	connectionRecovery
)

// Describe translates Jellyfin failures into safe, actionable instructions.
// A nil error presents a new attempt. It reads only immutable configuration,
// so it can run during Connect. It never displays raw error messages.
func (c *Connector) Describe(err error) connection.Presentation {
	if err == nil {
		return connection.Presentation{Kind: connection.SetupConnecting, Title: "Connecting to Jellyfin", Message: "Checking your connection and saved sign-in."}
	}
	stage := connectionAuthentication
	var failure *connectionError
	if errors.As(err, &failure) {
		stage = failure.stage
	}
	p := connection.Presentation{Kind: connection.SetupFailure, Retry: "Retry", Path: c.ConfigPath, PathLabel: "Configuration file",
		Title: titleConnectFailed, Message: messageConnectFailed}
	switch {
	case stage == connectionRecovery:
		p.Title, p.Message = titleRecoveryFailed, messageRecoveryFailed
		p.Path = ""
	case stage == connectionDiscovery:
		p.Title, p.Message = titleDiscoveryFailed, messageDiscoveryFailed
		if errors.Is(err, errNoServers) {
			p.Title = titleNoServers
		}
		p.Path = c.SettingsPath
	case stage == connectionServerStorage:
		p.Title, p.Message = titleSavedServerInvalid, messageSavedServerInvalid
		p.Path, p.PathLabel = filepath.Join(c.StateDir, "jellyfin-server.json"), "Saved server file"
	case stage == connectionConfig:
		p.Title, p.Message = connection.ConfigurationTitle, messageConfigInvalid
		if errors.Is(err, os.ErrNotExist) {
			p.Title, p.Message = titleSetupNeeded, messageSetupNeeded
		} else {
			var fileErr *os.PathError
			if errors.As(err, &fileErr) {
				p.Title, p.Message = titleConfigUnreadable, messageConfigUnreadable
			}
		}
	case stage == connectionSession || errors.Is(err, jellyfin.ErrSessionSave):
		p.Path, p.PathLabel = c.StateDir, "Sign-in folder"
		p.Title, p.Message = connection.SignInStorageTitle, connection.SignInStorageMessage
	case errors.Is(err, jellyfin.ErrQuickConnectExpired):
		p.Path, p.Retry = "", "New code"
		p.Title, p.Message = connection.CodeExpiredTitle, messageCodeExpired
	case errors.Is(err, jellyfin.ErrQuickConnectDisabled):
		p.Title, p.Message = titleQuickConnectDisabled, messageQuickConnectDisabled
	case errors.Is(err, jellyfin.ErrUsernameNotFound):
		p.Title, p.Message = titleUsernameInvalid, messageUsernameInvalid
	case jellyfin.Rejected(err):
		p.Retry = "Sign in"
		p.Title, p.Message = connection.SignInRequiredTitle, messageSignInRejected
	}
	if p.Path != "" {
		if absolute, err := filepath.Abs(p.Path); err == nil {
			p.Path = absolute
		}
	}
	return p
}

// approval presents only the public code and explains any recovered session.
func approval(code string, recovered bool) connection.Presentation {
	p := connection.Presentation{Kind: connection.SetupApproval, Title: "Quick Connect", Code: code, Recovered: recovered, Retry: "New code",
		Message: "In a signed-in Jellyfin client, open Quick Connect.\nEnter this code to approve MiSTerVision."}
	if recovered {
		p.Message = messageSignInRecovered
	}
	return p
}
