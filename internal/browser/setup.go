package browser

import (
	"errors"
	"os"
	"path/filepath"

	"misterfin-crt/internal/jellyfin"
	"misterfin-crt/internal/rendering"
)

// connectionStage identifies which operation failed before authentication ended.
type connectionStage uint8

const (
	connectionConfig connectionStage = iota
	connectionSession
	connectionAuthentication
)

// setupFailure classifies known failures by identity and operation. Unknown
// messages stay private. Relative paths are resolved exactly as file I/O sees them.
func setupFailure(stage connectionStage, err error, config Config) rendering.SetupPresentation {
	s := rendering.SetupPresentation{Kind: rendering.SetupConnectionFailed, Path: config.ConfigPath}
	switch {
	case stage == connectionConfig:
		s.Kind = rendering.SetupConfigInvalid
		if errors.Is(err, os.ErrNotExist) {
			s.Kind = rendering.SetupConfigMissing
		} else {
			var fileErr *os.PathError
			if errors.As(err, &fileErr) {
				s.Kind = rendering.SetupConfigUnreadable
			}
		}
	case stage == connectionSession || errors.Is(err, jellyfin.ErrSessionSave):
		s.Kind, s.Path = rendering.SetupSessionUnavailable, config.StateDir
	case errors.Is(err, jellyfin.ErrQuickConnectExpired):
		s.Kind = rendering.SetupCodeExpired
		s.Path = ""
	case errors.Is(err, jellyfin.ErrQuickConnectDisabled):
		s.Kind = rendering.SetupQuickConnectDisabled
	case errors.Is(err, jellyfin.ErrUsernameNotFound):
		s.Kind = rendering.SetupUsernameMissing
	case jellyfin.Rejected(err):
		s.Kind = rendering.SetupSignInRequired
	}
	if s.Path != "" {
		if absolute, e := filepath.Abs(s.Path); e == nil {
			s.Path = absolute
		}
	}
	return s
}
