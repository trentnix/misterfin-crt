package browser

import (
	"errors"
	"os"
	"path/filepath"

	"misterfin-crt/internal/jellyfin"
)

// SetupKind identifies a connection or setup screen without parsing UI text.
// SetupHidden leaves ordinary browsing visible.
type SetupKind uint8

const (
	SetupHidden               SetupKind = iota // Ordinary browsing is visible.
	SetupConnecting                            // Configuration loading or authentication is in progress.
	SetupQuickConnect                          // A public approval code is awaiting authorization.
	SetupConfigMissing                         // The selected Jellyfin configuration file does not exist.
	SetupConfigInvalid                         // The file was read but its settings are invalid.
	SetupConfigUnreadable                      // The selected configuration file cannot be opened.
	SetupConnectionFailed                      // The server or transport could not complete sign-in.
	SetupSignInRequired                        // Jellyfin explicitly rejected authentication.
	SetupCodeExpired                           // The approval deadline elapsed.
	SetupQuickConnectDisabled                  // Server policy requires enabling Quick Connect or an API key.
	SetupUsernameMissing                       // An API key was accepted but its configured user was not found.
	SetupSessionUnavailable                    // Saved sign-in storage could not be read or written.
)

// SetupPresentation is a copied snapshot for shared setup rendering. Code is
// the public approval code, never the Quick Connect secret. Path identifies the
// configuration file or sign-in folder. No raw errors or credentials belong here.
type SetupPresentation struct {
	Kind SetupKind
	Code string
	Path string
}

// retryLabel describes the existing open/retry action for the current state.
// An empty label means an attempt is already running and cannot be retried yet.
func (s SetupPresentation) retryLabel() string {
	switch s.Kind {
	case SetupHidden, SetupConnecting:
		return ""
	case SetupQuickConnect, SetupCodeExpired:
		return "New code"
	case SetupSignInRequired:
		return "Sign in"
	default:
		return "Retry"
	}
}

// content supplies actionable text without showing raw server or file errors.
func (s SetupPresentation) content() (string, string) {
	switch s.Kind {
	case SetupConnecting:
		return "Connecting to Jellyfin", "Checking your connection and saved sign-in."
	case SetupQuickConnect:
		return "Quick Connect", "In a signed-in Jellyfin client, open Quick Connect.\nEnter this code to approve MiSTerFin CRT."
	case SetupConfigMissing:
		return "Setup needed", "Create this file and add your Jellyfin server address.\nFor example: http://192.168.1.10:8096"
	case SetupConfigInvalid:
		return "Check your configuration", "Use an HTTP or HTTPS server address on the first line.\nCheck any optional settings, then retry."
	case SetupConfigUnreadable:
		return "Can't read configuration", "Make sure this file exists and is readable, then retry."
	case SetupCodeExpired:
		return "Code expired", "Request a new code, then approve it in Jellyfin."
	case SetupQuickConnectDisabled:
		return "Quick Connect is disabled", "Enable Quick Connect on your Jellyfin server, then retry.\nOr add an API key and username to your configuration."
	case SetupUsernameMissing:
		return "Check your username", "The configured username was not found on the server.\nCheck the username after your API key, then retry."
	case SetupSessionUnavailable:
		return "Can't save or read sign-in", "Make sure this folder is writable, then retry.\nYour saved sign-in has not been cleared."
	case SetupSignInRequired:
		return "Sign-in required", "Jellyfin rejected your sign-in. Sign in again.\nIf you use an API key, check it in your configuration."
	default:
		return "Can't connect to Jellyfin", "Check your server address and network connection.\nMake sure Jellyfin is running, then retry."
	}
}

// connectionStage identifies which operation failed before authentication ended.
type connectionStage uint8

const (
	connectionConfig connectionStage = iota
	connectionSession
	connectionAuthentication
)

// setupFailure classifies known failures by identity and operation. Unknown
// messages stay private. Relative paths are resolved exactly as file I/O sees them.
func setupFailure(stage connectionStage, err error, config Config) SetupPresentation {
	s := SetupPresentation{Kind: SetupConnectionFailed, Path: config.ConfigPath}
	switch {
	case stage == connectionConfig:
		s.Kind = SetupConfigInvalid
		if errors.Is(err, os.ErrNotExist) {
			s.Kind = SetupConfigMissing
		} else {
			var fileErr *os.PathError
			if errors.As(err, &fileErr) {
				s.Kind = SetupConfigUnreadable
			}
		}
	case stage == connectionSession || errors.Is(err, jellyfin.ErrSessionSave):
		s.Kind, s.Path = SetupSessionUnavailable, config.StateDir
	case errors.Is(err, jellyfin.ErrQuickConnectExpired):
		s.Kind = SetupCodeExpired
		s.Path = ""
	case errors.Is(err, jellyfin.ErrQuickConnectDisabled):
		s.Kind = SetupQuickConnectDisabled
	case errors.Is(err, jellyfin.ErrUsernameNotFound):
		s.Kind = SetupUsernameMissing
	case jellyfin.Rejected(err):
		s.Kind = SetupSignInRequired
	}
	if s.Path != "" {
		if absolute, e := filepath.Abs(s.Path); e == nil {
			s.Path = absolute
		}
	}
	return s
}
