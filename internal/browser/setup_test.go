package browser

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"misterfin-crt/internal/input/control"
	"misterfin-crt/internal/jellyfin"
	"misterfin-crt/internal/rendering"
)

func TestSetupFailuresHaveSpecificRecoveryWithoutRawErrors(t *testing.T) {
	config := Config{ConfigPath: "selected/jellyfin.conf", StateDir: "selected/state"}
	for _, tc := range []struct {
		stage connectionStage
		err   error
		kind  rendering.SetupKind
	}{
		{connectionConfig, fmt.Errorf("private URL: %w", os.ErrNotExist), rendering.SetupConfigMissing},
		{connectionConfig, &os.PathError{Op: "open", Path: "private-path", Err: os.ErrPermission}, rendering.SetupConfigUnreadable},
		{connectionConfig, errors.New("private configuration content"), rendering.SetupConfigInvalid},
		{connectionSession, errors.New("private token"), rendering.SetupSessionUnavailable},
		{connectionAuthentication, jellyfin.ErrSessionSave, rendering.SetupSessionUnavailable},
		{connectionAuthentication, jellyfin.ErrUsernameNotFound, rendering.SetupUsernameMissing},
		{connectionAuthentication, jellyfin.ErrQuickConnectDisabled, rendering.SetupQuickConnectDisabled},
		{connectionAuthentication, fmt.Errorf("private secret: %w", jellyfin.ErrQuickConnectExpired), rendering.SetupCodeExpired},
		{connectionAuthentication, &jellyfin.HTTPError{Status: 401}, rendering.SetupSignInRequired},
		{connectionAuthentication, &jellyfin.HTTPError{Status: 500}, rendering.SetupConnectionFailed},
	} {
		s := setupFailure(tc.stage, tc.err, config)
		if s.Kind != tc.kind {
			t.Fatalf("got %v, want %v", s.Kind, tc.kind)
		}
		if strings.Contains(s.Path+s.Code, "private") {
			t.Fatal("raw failure leaked to presentation")
		}
		if s.Kind != rendering.SetupCodeExpired && !filepath.IsAbs(s.Path) {
			t.Fatal("selected path was not resolved")
		}
		if s.RetryLabel() == "" {
			t.Fatal("failure has no recovery action")
		}
	}
}

func TestSetupRetryDoesNotRestartAnActiveConnection(t *testing.T) {
	s := testSession(t)
	s.controller.running = false
	t.Cleanup(s.connection.close)
	s.setup = rendering.SetupPresentation{Kind: rendering.SetupConnecting}
	for _, key := range []control.Action{"open", "retry", "open-repeat"} {
		s.dispatchKey(key)
		if s.connection.generation != 0 {
			t.Fatal("input restarted an active attempt")
		}
	}
	s.setup = rendering.SetupPresentation{Kind: rendering.SetupCodeExpired}
	s.dispatchKey(control.Open)
	if s.connection.generation != 1 || s.setup.Kind != rendering.SetupConnecting || s.setup.Code != "" {
		t.Fatal("new-code action did not replace expired code")
	}
	s.handleAuthCode(authCodeResult{generation: 0, code: "stale"})
	if s.setup.Kind != rendering.SetupConnecting {
		t.Fatal("old approval code replaced current attempt")
	}
	s.handleAuth(authResult{generation: 0, err: jellyfin.ErrQuickConnectExpired})
	if s.setup.Kind != rendering.SetupConnecting {
		t.Fatal("old failure replaced current attempt")
	}
	s.dispatchKey(control.Back)
	if !s.model.Quit {
		t.Fatal("exit was not available while connecting")
	}
}

func TestConnectionReloadsConfigurationAndReportsItsFailureStage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/Users" {
			t.Errorf("unexpected request %s", r.URL.Path)
			w.WriteHeader(404)
			return
		}
		fmt.Fprint(w, `[{"Id":"user","Name":"viewer"}]`)
	}))
	defer server.Close()
	dir := t.TempDir()
	config := Config{ConfigPath: filepath.Join(dir, "jellyfin.conf"), StateDir: dir}
	m := newConnectionManager(config, 640, 240)
	defer m.close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	results := make(chan workerResult, 4)
	send := func(work context.Context, r workerResult) {
		select {
		case results <- r:
		case <-work.Done():
		}
	}
	receive := func() authResult {
		t.Helper()
		select {
		case r := <-results:
			result, ok := r.(authResult)
			if !ok {
				t.Fatal(r)
			}
			return result
		case <-ctx.Done():
			t.Fatal("connection did not finish")
			return authResult{}
		}
	}
	m.connect(ctx, send)
	failed := receive()
	if failed.stage != connectionConfig || setupFailure(failed.stage, failed.err, config).Kind != rendering.SetupConfigMissing {
		t.Fatal("missing configuration misclassified")
	}
	if err := os.WriteFile(config.ConfigPath, []byte(server.URL+"\napi-key\nviewer\n"), 0600); err != nil {
		t.Fatal(err)
	}
	m.connect(ctx, send)
	ready := receive()
	if ready.err != nil || ready.connection == nil || ready.generation == failed.generation {
		t.Fatal("retry did not reload repaired configuration")
	}
}

func TestQuickConnectPublishesOnlyApprovalCodeAndCanBeReplaced(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/QuickConnect/Enabled":
			fmt.Fprint(w, "true")
		case "/QuickConnect/Initiate":
			fmt.Fprint(w, `{"Code":"123456","Secret":"private-secret"}`)
		default:
			w.WriteHeader(500)
		}
	}))
	defer server.Close()
	dir := t.TempDir()
	config := Config{ConfigPath: filepath.Join(dir, "jellyfin.conf"), StateDir: dir}
	if err := os.WriteFile(config.ConfigPath, []byte(server.URL), 0600); err != nil {
		t.Fatal(err)
	}
	s := testSession(t)
	s.controller.running = false
	s.config = config
	s.connection = newConnectionManager(config, 640, 240)
	defer s.connection.close()
	for attempt := 1; attempt <= 2; attempt++ {
		if attempt == 1 {
			s.authenticate()
		} else {
			s.dispatchKey(control.Open)
		}
		deadline := time.After(time.Second)
		for s.setup.Kind != rendering.SetupQuickConnect {
			select {
			case result := <-s.events:
				s.handleResult(result)
			case <-deadline:
				t.Fatal("approval code not published")
			}
		}
		if s.setup.Code != "123456" || s.connection.generation != attempt {
			t.Fatal("new-code flow did not replace request")
		}
	}
}
