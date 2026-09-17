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
)

// Describe translates Jellyfin failures into safe, actionable instructions.
// A nil error presents a new attempt. It never displays raw error messages.
func (c Connector) Describe(err error) connection.Presentation {
	if err == nil {
		return connection.Presentation{Kind: connection.SetupConnecting, Title: "Connecting to Jellyfin", Message: "Checking your connection and saved sign-in."}
	}
	stage := connectionAuthentication
	var failure *connectionError
	if errors.As(err, &failure) {
		stage = failure.stage
	}
	p := connection.Presentation{Kind: connection.SetupFailure, Retry: "Retry", Path: c.ConfigPath, PathLabel: "Configuration file",
		Title: "Can't connect to Jellyfin", Message: "Check your server address and network connection.\nMake sure Jellyfin is running, then retry."}
	switch {
	case stage == connectionConfig:
		p.Title, p.Message = "Check your configuration", "Use an HTTP or HTTPS server address on the first line.\nCheck any optional settings, then retry."
		if errors.Is(err, os.ErrNotExist) {
			p.Title, p.Message = "Setup needed", "Create this file and add your Jellyfin server address.\nFor example: http://192.168.1.10:8096"
		} else {
			var fileErr *os.PathError
			if errors.As(err, &fileErr) {
				p.Title, p.Message = "Can't read configuration", "Make sure this file exists and is readable, then retry."
			}
		}
	case stage == connectionSession || errors.Is(err, jellyfin.ErrSessionSave):
		p.Path, p.PathLabel = c.StateDir, "Sign-in folder"
		p.Title, p.Message = "Can't save or read sign-in", "Make sure this folder is writable, then retry.\nYour saved sign-in has not been cleared."
	case errors.Is(err, jellyfin.ErrQuickConnectExpired):
		p.Path, p.Retry = "", "New code"
		p.Title, p.Message = "Code expired", "Request a new code, then approve it in Jellyfin."
	case errors.Is(err, jellyfin.ErrQuickConnectDisabled):
		p.Title, p.Message = "Quick Connect is disabled", "Enable Quick Connect on your Jellyfin server, then retry.\nOr add an API key and username to your configuration."
	case errors.Is(err, jellyfin.ErrUsernameNotFound):
		p.Title, p.Message = "Check your username", "The configured username was not found on the server.\nCheck the configured username, then retry."
	case jellyfin.Rejected(err):
		p.Retry = "Sign in"
		p.Title, p.Message = "Sign-in required", "Jellyfin rejected your sign-in. Sign in again.\nIf you use an API key, check it in your configuration."
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
		p.Message = "Saved sign-in was damaged and backed up.\nOpen Quick Connect in Jellyfin and approve this code."
	}
	return p
}
