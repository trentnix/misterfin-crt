package browser

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"mistervision/internal/connection"
	"mistervision/internal/input/control"
	jfconnection "mistervision/internal/jellyfin/connection"
)

type choosingConnector struct {
	chosen  chan connection.Server
	started chan bool
}

func (c choosingConnector) Connect(ctx context.Context, interaction connection.Interaction) (connection.Session, error) {
	c.started <- interaction.SelectServer
	selected, err := interaction.ChooseServer(ctx, []connection.Server{
		{ID: "a", Name: "First", URL: "http://first"}, {ID: "b", Name: "Second", URL: "http://second"},
	})
	if err != nil {
		return connection.Session{}, err
	}
	c.chosen <- selected
	interaction.Show(connection.Presentation{Kind: connection.SetupApproval, Code: "123456", Retry: "New code"})
	<-ctx.Done()
	return connection.Session{}, ctx.Err()
}
func (choosingConnector) Describe(err error) connection.Presentation {
	if err != nil {
		return connection.Presentation{Kind: connection.SetupFailure, Retry: "Retry"}
	}
	return connection.Presentation{Kind: connection.SetupConnecting}
}

func TestServerPickerSelectionRetryAndCancellation(t *testing.T) {
	s := testSession(t)
	s.controller.running = false
	connector := choosingConnector{chosen: make(chan connection.Server, 2), started: make(chan bool, 4)}
	s.config = Config{Connector: connector}
	s.connection = newConnectionManager(s.config, 640, 240)
	defer s.connection.close()
	wait := func(kind connection.SetupKind) {
		t.Helper()
		timeout := time.After(time.Second)
		for s.setup.Kind != kind {
			select {
			case r := <-s.events:
				s.handleResult(r)
			case <-timeout:
				t.Fatal("setup did not progress")
			}
		}
	}
	s.authenticate()
	wait(connection.SetupServers)
	s.dispatchKey(control.Up)
	if s.setup.Selected != 0 {
		t.Fatal("selection moved before first server")
	}
	s.dispatchKey(control.Down)
	s.dispatchKey(control.Down)
	if s.setup.Selected != 1 {
		t.Fatal("selection did not clamp to final server")
	}
	if (serverChoicesResult{generation: 0, servers: nil}).apply(s) || len(s.setup.Servers) != 2 {
		t.Fatal("stale picker replaced candidates")
	}
	s.dispatchKey(control.Open)
	s.dispatchKey(control.Open)
	wait(connection.SetupApproval)
	select {
	case server := <-connector.chosen:
		if server.ID != "b" {
			t.Fatal("wrong server selected")
		}
	default:
		t.Fatal("selection was not delivered")
	}
	s.dispatchKey(control.Open)
	wait(connection.SetupServers)
	generation := s.connection.generation
	s.dispatchKey(control.Retry)
	wait(connection.SetupServers)
	if s.connection.generation != generation+1 {
		t.Fatal("scan again did not replace attempt")
	}
	s.dispatchKey(control.Back)
	if !s.model.Quit {
		t.Fatal("Back did not exit setup")
	}
	// close must join the worker even while it is waiting for a selection.
	done := make(chan struct{})
	go func() { s.connection.close(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("picker worker did not cancel")
	}
}

// TestSetupBackCancelsSelectedServer covers every screen between choosing a
// server and authenticating, including queued results from the canceled attempt.
func TestSetupBackCancelsSelectedServer(t *testing.T) {
	for _, stage := range []connection.SetupKind{connection.SetupConnecting, connection.SetupApproval, connection.SetupFailure} {
		t.Run(fmt.Sprint(stage), func(t *testing.T) {
			s := testSession(t)
			s.controller.running = false
			connector := choosingConnector{chosen: make(chan connection.Server, 4), started: make(chan bool, 4)}
			s.config = Config{Connector: connector}
			s.connection = newConnectionManager(s.config, 640, 240)
			defer s.connection.close()
			wait := func(kind connection.SetupKind) {
				t.Helper()
				timeout := time.After(time.Second)
				for s.setup.Kind != kind {
					select {
					case r := <-s.events:
						s.handleResult(r)
					case <-timeout:
						t.Fatal("setup did not progress")
					}
				}
			}
			s.authenticate()
			wait(connection.SetupServers)
			if <-connector.started {
				t.Fatal("initial connection forced discovery")
			}
			s.dispatchKey(control.Down)
			s.dispatchKey(control.Open)
			generation := s.connection.generation
			if stage != connection.SetupConnecting {
				wait(connection.SetupApproval)
			}
			if stage == connection.SetupFailure {
				s.handleAuth(authResult{generation: generation, err: errors.New("sign-in failed")})
			}
			if !s.setup.BackToServers {
				t.Fatal("selected-server setup does not offer Back")
			}
			s.dispatchKey(control.Back)
			if s.model.Quit || !s.connection.selectServer || s.setup.BackToServers {
				t.Fatal("Back did not start a fresh server choice")
			}
			wait(connection.SetupServers)
			if !<-connector.started {
				t.Fatal("Back did not request discovery from the connector")
			}
			s.handleAuthCode(authCodeResult{generation: generation, presentation: connection.Presentation{Kind: connection.SetupApproval, Code: "stale"}})
			s.handleAuth(authResult{generation: generation, err: errors.New("canceled")})
			if s.setup.Kind != connection.SetupServers {
				t.Fatal("old sign-in replaced the picker")
			}
			s.dispatchKey(control.Open)
			wait(connection.SetupApproval)
			if !s.setup.BackToServers || s.connection.selectServer {
				t.Fatal("new selection did not restore sign-in navigation")
			}
			s.dispatchKey(control.Back)
			wait(connection.SetupServers)
			s.dispatchKey(control.Back)
			if !s.model.Quit {
				t.Fatal("picker no longer permits exit")
			}
		})
	}
}

// setupDiscoverer lets setup tests use the real connector without UDP broadcasts.
type setupDiscoverer func(context.Context) ([]connection.Server, error)

func (f setupDiscoverer) Discover(ctx context.Context) ([]connection.Server, error) { return f(ctx) }

// TestDiscoveryBackAfterRelaunch exercises persisted selection through the real
// Jellyfin connector and browser, including new approval codes and sign-in errors.
func TestDiscoveryBackAfterRelaunch(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		t.Run(fmt.Sprintf("quick-connect-enabled=%t", enabled), func(t *testing.T) {
			var codes, scans atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/QuickConnect/Enabled":
					fmt.Fprint(w, enabled)
				case "/QuickConnect/Initiate":
					fmt.Fprintf(w, `{"Code":"%06d","Secret":"private-secret"}`, codes.Add(1))
				case "/QuickConnect/Connect":
					fmt.Fprint(w, `{"Authenticated":false}`)
				default:
					t.Errorf("unexpected request %s", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()
			dir := t.TempDir()
			candidate := connection.Server{ID: "server", Name: "Test server", URL: server.URL}
			connector := jfconnection.Connector{StateDir: dir, ConfigPath: filepath.Join(dir, "jellyfin.conf"), Discovery: setupDiscoverer(func(context.Context) ([]connection.Server, error) {
				scans.Add(1)
				return []connection.Server{candidate}, nil
			})}
			for launch := range 2 {
				func() {
					s := testSession(t)
					s.controller.running = false
					s.config = Config{Connector: connector}
					s.connection = newConnectionManager(s.config, 640, 240)
					defer s.connection.close()
					wait := func(kind connection.SetupKind) {
						t.Helper()
						timer := time.NewTimer(3 * time.Second)
						defer timer.Stop()
						for s.setup.Kind != kind {
							select {
							case result := <-s.events:
								s.handleResult(result)
							case <-timer.C:
								t.Fatalf("launch %d: expected %v, got %v", launch, kind, s.setup.Kind)
							}
						}
					}
					s.authenticate()
					if launch == 0 {
						wait(connection.SetupServers)
						s.dispatchKey(control.Open)
					}
					kind := connection.SetupFailure
					if enabled {
						kind = connection.SetupApproval
					}
					wait(kind)
					if !s.setup.BackToServers {
						t.Fatalf("launch %d: remembered discovery sign-in offers Exit instead of Back", launch)
					}
					if enabled {
						previous := s.setup.Code
						s.dispatchKey(control.Open)
						wait(connection.SetupApproval)
						if s.setup.Code == previous || !s.setup.BackToServers {
							t.Fatal("new code lost Back navigation")
						}
					}
					// Relaunch and New code must reuse the choice until Back is pressed.
					expectedScans := int32(1 + launch)
					if scans.Load() != expectedScans {
						t.Fatal("sign-in unexpectedly rescanned")
					}
					s.dispatchKey(control.Back)
					if s.model.Quit {
						t.Fatal("Back exited instead of returning to discovery")
					}
					wait(connection.SetupServers)
					if s.setup.BackToServers {
						t.Fatal("picker must offer Exit")
					}
					s.dispatchKey(control.Back)
					if !s.model.Quit {
						t.Fatal("Exit did not close the picker")
					}
				}()
			}
			if scans.Load() != 3 {
				t.Fatalf("expected three scans, got %d", scans.Load())
			}
		})
	}
}
