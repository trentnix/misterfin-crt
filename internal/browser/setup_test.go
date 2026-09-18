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
	"sync/atomic"
	"testing"
	"time"

	"mistervision/internal/connection"
	"mistervision/internal/input/control"
	"mistervision/internal/jellyfin"
	jfconnection "mistervision/internal/jellyfin/connection"
	"mistervision/internal/rendering"
)

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
	s.setup = rendering.SetupPresentation{Kind: rendering.SetupFailure, Retry: "New code"}
	s.dispatchKey(control.Open)
	if s.connection.generation != 1 || s.setup.Kind != rendering.SetupConnecting || s.setup.Code != "" {
		t.Fatal("new-code action did not replace expired code")
	}
	s.handleAuthCode(authCodeResult{generation: 0, presentation: rendering.SetupPresentation{Code: "stale"}})
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
	options := &jfconnection.Connector{ConfigPath: filepath.Join(dir, "jellyfin.conf"), StateDir: dir}
	config := Config{Connector: options}
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
	if config.Connector.Describe(failed.err).Title != "Setup needed" {
		t.Fatal("missing configuration misclassified")
	}
	if err := os.WriteFile(options.ConfigPath, []byte(server.URL+"\napi-key\nviewer\n"), 0600); err != nil {
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
	options := &jfconnection.Connector{ConfigPath: filepath.Join(dir, "jellyfin.conf"), StateDir: dir}
	config := Config{Connector: options}
	if err := os.WriteFile(options.ConfigPath, []byte(server.URL), 0600); err != nil {
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
		for s.setup.Kind != rendering.SetupApproval {
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
		if s.setup.Back == connection.BackServers {
			t.Fatal("explicit configuration unexpectedly offers discovery navigation")
		}
	}
}

func TestRecoveredSessionReachesAuthenticationAndPresentation(t *testing.T) {
	for _, apiKey := range []bool{false, true} {
		t.Run(fmt.Sprint("api-key=", apiKey), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !strings.Contains(r.Header.Get("Authorization"), `Version="v2.3.4"`) {
					t.Error("build identity not propagated")
				}
				switch r.URL.Path {
				case "/Users":
					fmt.Fprint(w, `[{"Id":"user","Name":"viewer"}]`)
				case "/QuickConnect/Enabled":
					fmt.Fprint(w, "true")
				case "/QuickConnect/Initiate":
					fmt.Fprint(w, `{"Code":"123456","Secret":"private"}`)
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()
			dir := t.TempDir()
			options := &jfconnection.Connector{ConfigPath: filepath.Join(dir, "jellyfin.conf"), StateDir: dir}
			config := Config{Connector: options}
			options.Version = "v2.3.4"
			config.Connector = options
			text := server.URL
			if apiKey {
				text += "\nkey\nviewer\n"
			}
			if err := os.WriteFile(options.ConfigPath, []byte(text), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "session.json"), []byte("broken"), 0600); err != nil {
				t.Fatal(err)
			}
			m := newConnectionManager(config, 640, 240)
			defer m.close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			results := make(chan workerResult, 4)
			m.connect(ctx, func(work context.Context, r workerResult) {
				select {
				case results <- r:
				case <-work.Done():
				}
			})
			select {
			case result := <-results:
				s := testSession(t)
				s.connection.generation = 1
				if apiKey {
					r, ok := result.(authResult)
					if !ok || r.err != nil || !r.connection.recovered {
						t.Fatalf("API-key recovery failed: %#v", result)
					}

				} else {
					r, ok := result.(authCodeResult)
					if !ok || !r.presentation.Recovered {
						t.Fatalf("Quick Connect recovery failed: %#v", result)
					}
					s.handleAuthCode(r)
					if !s.setup.Recovered || s.setup.Kind != rendering.SetupApproval || s.setup.Code != "123456" {
						t.Fatal("recovery was not presented with the new code")
					}
				}
			case <-time.After(3 * time.Second):
				t.Fatal("damaged session blocked authentication")
			}
		})
	}
}

// scriptedConnector tests the browser boundary without a server adapter.
type scriptedConnector struct {
	connect func(context.Context, func(connection.Presentation)) (connection.Session, error)
}

func (c scriptedConnector) Connect(ctx context.Context, interaction connection.Interaction) (connection.Session, error) {
	return c.connect(ctx, interaction.Show)
}
func (scriptedConnector) Describe(err error) connection.Presentation {
	if err == nil {
		return connection.Presentation{Kind: connection.SetupConnecting, Title: "Connecting to Example"}
	}
	return connection.Presentation{Kind: connection.SetupFailure, Title: "Example unavailable", Message: "Try again later."}
}

func TestInjectedConnectorOwnsPresentationAndAttemptsDoNotOverlap(t *testing.T) {
	var active atomic.Int32
	s := testSession(t)
	s.controller.running = false
	config := Config{Connector: scriptedConnector{connect: func(ctx context.Context, progress func(connection.Presentation)) (connection.Session, error) {
		if active.Add(1) != 1 {
			t.Error("connection attempts overlap")
		}
		defer active.Add(-1)
		progress(connection.Presentation{Kind: connection.SetupApproval, Title: "Link Example", Message: "Visit example.test/link", Code: "EXAMPLE", Retry: "New code"})
		<-ctx.Done()
		return connection.Session{}, ctx.Err()
	}}}
	s.config = config
	s.connection = newConnectionManager(config, 640, 240)
	defer s.connection.close()
	for attempt := 1; attempt <= 2; attempt++ {
		s.authenticate()
		if s.setup.Title != "Connecting to Example" {
			t.Fatal("browser supplied its own provider text")
		}
		timeout := time.After(time.Second)
		for s.setup.Kind != rendering.SetupApproval {
			select {
			case result := <-s.events:
				s.handleResult(result)
			case <-timeout:
				t.Fatal("connector progress not delivered")
			}
		}
		if s.setup.Title != "Link Example" || s.setup.Message != "Visit example.test/link" || s.setup.Code != "EXAMPLE" {
			t.Fatal("connector presentation was changed")
		}
	}
	s.handleAuth(authResult{generation: s.connection.generation, err: errors.New("private URL and token")})
	if s.setup.Title != "Example unavailable" || s.setup.Message != "Try again later." {
		t.Fatal("safe connector failure was not presented")
	}
}
