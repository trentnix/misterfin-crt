package connection

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mistervision/internal/connection"
	"mistervision/internal/jellyfin"
)

func TestSetupFailuresHaveSpecificRecoveryWithoutRawErrors(t *testing.T) {
	config := Connector{ConfigPath: "selected/jellyfin.conf", StateDir: "selected/state"}
	for _, tc := range []struct {
		stage connectionStage
		err   error
		title string
	}{
		{connectionConfig, fmt.Errorf("private URL: %w", os.ErrNotExist), "Setup needed"},
		{connectionConfig, &os.PathError{Op: "open", Path: "private-path", Err: os.ErrPermission}, "Can't read configuration"},
		{connectionConfig, errors.New("private configuration content"), "Check your configuration"},
		{connectionSession, errors.New("private token"), "Can't save or read sign-in"},
		{connectionAuthentication, jellyfin.ErrSessionSave, "Can't save or read sign-in"},
		{connectionAuthentication, jellyfin.ErrUsernameNotFound, "Check your username"},
		{connectionAuthentication, jellyfin.ErrQuickConnectDisabled, "Quick Connect is disabled"},
		{connectionAuthentication, fmt.Errorf("private secret: %w", jellyfin.ErrQuickConnectExpired), "Code expired"},
		{connectionAuthentication, &jellyfin.HTTPError{Status: 401}, "Sign-in required"},
		{connectionAuthentication, &jellyfin.HTTPError{Status: 500}, "Can't connect to Jellyfin"},
	} {
		s := config.Describe(&connectionError{tc.stage, tc.err})
		if s.Kind != connection.SetupFailure || s.Title != tc.title {
			t.Fatalf("got %v, want %v", s.Title, tc.title)
		}
		if strings.Contains(s.Title+s.Message+s.Path+s.Code, "private") {
			t.Fatal("raw failure leaked to presentation")
		}
		if s.Title != "Code expired" && !filepath.IsAbs(s.Path) {
			t.Fatal("selected path was not resolved")
		}
		if s.RetryLabel() == "" {
			t.Fatal("failure has no recovery action")
		}
	}
}
