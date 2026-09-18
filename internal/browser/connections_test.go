package browser

import (
	"context"
	"errors"
	"fmt"
	"mistervision/internal/connection"
	"mistervision/internal/input/control"
	"mistervision/internal/media"
	"mistervision/internal/playback"
	"mistervision/internal/remote"
	"mistervision/internal/rendering"
	"sync"
	"testing"
	"time"
)

func TestConnectionMenuBackAndUnavailableChoicePreserveSession(t *testing.T) {
	s := testSession(t)
	s.controller.running = false
	s.about.Visible = true
	s.about.Connections = []connection.Choice{{Name: "Use existing connection", Children: []connection.Choice{{ID: "one", Name: "One"}}}, {Name: "Plex", Help: "Configure Plex"}}
	s.handleAboutKey(control.Down)
	s.handleAboutKey(control.Open)
	if len(s.about.ConnectionPath) != 1 {
		t.Fatal("existing submenu did not open")
	}
	s.handleAboutKey(control.Back)
	s.handleAboutKey(control.Down)
	s.handleAboutKey(control.Open)
	if s.about.ConnectionMessage != "Configure Plex" || s.connectionChange != nil || s.model.Quit {
		t.Fatal("setup help replaced active connection")
	}
	s.handleAboutKey(control.Up)
	s.handleAboutKey(control.Open)
	s.handleAboutKey(control.Open)
	if s.connectionChange == nil || s.connectionChange.ID != "one" || !s.model.Quit {
		t.Fatal("selection did not request a clean session handoff")
	}
}

type switchServer struct {
	media.Server
	id string
}

func (s switchServer) Identity() media.Identity                             { return media.Identity{Server: s.id, User: "viewer"} }
func (s switchServer) Libraries(context.Context) (media.Page, error)        { return media.Page{}, nil }
func (s switchServer) ContinueWatching(context.Context) (media.Page, error) { return media.Page{}, nil }

type switchConnector struct {
	server media.Server
	source remote.Source
}

func (c switchConnector) Connect(context.Context, connection.Interaction) (connection.Session, error) {
	return connection.Session{Server: c.server, Remote: c.source}, nil
}
func (switchConnector) Describe(error) connection.Presentation {
	return connection.Presentation{Kind: connection.SetupConnecting}
}

type switchRemote struct {
	started, stopped chan struct{}
	mu               sync.Mutex
	emit             func(remote.Command)
}

func (s *switchRemote) Run(ctx context.Context, emit func(remote.Command)) {
	s.mu.Lock()
	s.emit = emit
	s.mu.Unlock()
	close(s.started)
	<-ctx.Done()
	emit(remote.Command{Kind: remote.Message, Text: "late command"})
	close(s.stopped)
}
func (*switchRemote) Publish(remote.QueueState) {}

// TestConnectionHandoffStopsRemoteBeforePlex verifies the old listener has
// stopped before the next browser opens. Late commands belong to the old queue.
func TestConnectionHandoffStopsRemoteBeforePlex(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Second)
	defer cancel()
	source := &switchRemote{started: make(chan struct{}), stopped: make(chan struct{})}
	keys := make(chan control.Event, 4)
	step := 0
	renderer := &controlCaptureRenderer{Renderer: rendering.NewRenderer(), capture: func(scene rendering.Scene) {
		if scene.Setup.Kind != rendering.SetupHidden {
			return
		}
		switch step {
		case 0:
			select {
			case <-source.started:
			case <-ctx.Done():
				t.Error("remote source did not start")
			}
			keys <- control.Event{Action: control.About}
			step++
		case 1:
			if scene.About.Visible {
				keys <- control.Event{Action: control.Down}
				step++
			}
		case 2:
			if scene.About.ConnectionsVisible {
				keys <- control.Event{Action: control.Open}
				step++
			}
		}
	}}
	cfg := Config{Connector: switchConnector{switchServer{id: "jellyfin"}, source}, Connections: connectionChoices(connection.Choice{ID: "plex", Name: "Plex"})}
	err := Run(ctx, cfg, playback.Config{}, &runTestOutput{}, renderer, nil, keys)
	var change *connection.Change
	if !errors.As(err, &change) || change.ID != "plex" {
		t.Fatalf("handoff: %v", err)
	}
	select {
	case <-source.stopped:
	default:
		t.Fatal("browser returned before remote listener stopped")
	}
	// Retained credentials do not imply an active listener. Even a stale callback
	// cannot deliver into the new browser's independently allocated event channel.
	source.mu.Lock()
	emit := source.emit
	source.mu.Unlock()
	emit(remote.Command{Kind: remote.Message, Text: "must not reach Plex"})
	seen := false
	renderer = &controlCaptureRenderer{Renderer: rendering.NewRenderer(), capture: func(scene rendering.Scene) {
		if scene.Message.Text != "" {
			t.Error("old Jellyfin command reached Plex")
		}
		if scene.Setup.Kind == rendering.SetupHidden && !seen {
			seen = true
			keys <- control.Event{Action: control.Quit}
		}
	}}
	err = Run(ctx, Config{Connector: switchConnector{server: switchServer{id: "plex"}}}, playback.Config{}, &runTestOutput{}, renderer, nil, keys)
	if err != nil || !seen || ctx.Err() != nil {
		t.Fatalf("Plex startup: %v", err)
	}
}

func TestNavigationBelongsToOneAccount(t *testing.T) {
	s := testSession(t)
	s.client = switchServer{id: "one"}
	s.config.Navigation = &Navigation{}
	s.connectionChange = &connection.Change{ID: "two"}
	s.model.Current().Selected = 5
	s.model.Current().Target = 8
	s.model.Quit = true
	s.rememberNavigation()
	other := testSession(t)
	other.config.Navigation = s.config.Navigation
	other.client = switchServer{id: "two"}
	other.restoreNavigation()
	if other.model.Current().Selected != 0 {
		t.Fatal("another account inherited navigation")
	}
	other.client = s.client
	other.restoreNavigation()
	if other.model.Current().Selected != 5 || other.model.Current().Target != 5 || other.model.Quit {
		t.Fatal("returning account lost navigation")
	}
}

// blockedConnector never authenticates. Switching must cancel and join its work.
type blockedConnector struct {
	kind    connection.SetupKind
	stopped chan struct{}
}

func (c blockedConnector) Connect(ctx context.Context, interaction connection.Interaction) (connection.Session, error) {
	defer close(c.stopped)
	interaction.Progress(connection.Presentation{Kind: c.kind, Title: "Waiting for server"})
	<-ctx.Done()
	return connection.Session{}, ctx.Err()
}

func (c blockedConnector) Describe(error) connection.Presentation {
	return connection.Presentation{Kind: connection.SetupConnecting}
}

func TestSwitchConnectionBeforeAuthentication(t *testing.T) {
	for _, kind := range []connection.SetupKind{connection.SetupConnecting, connection.SetupApproval, connection.SetupFailure, connection.SetupServers} {
		t.Run(fmt.Sprint(kind), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			connector := blockedConnector{kind: kind, stopped: make(chan struct{})}
			keys := make(chan control.Event, 1)
			step := 0
			renderer := &controlCaptureRenderer{Renderer: rendering.NewRenderer(), capture: func(scene rendering.Scene) {
				switch step {
				case 0:
					if scene.Setup.Kind == kind && scene.Setup.Title == "Waiting for server" {
						keys <- control.Event{Action: control.About}
						step++
					}
				case 1:
					if scene.About.Visible {
						keys <- control.Event{Action: control.Down}
						step++
					}
				case 2:
					if scene.About.ConnectionsVisible {
						keys <- control.Event{Action: control.Open}
						step++
					}
				}
			}}
			cfg := Config{Connector: connector, Connections: connectionChoices(connection.Choice{ID: "plex", Name: "Plex"})}
			err := Run(ctx, cfg, playback.Config{}, &runTestOutput{}, renderer, nil, keys)
			var change *connection.Change
			if ctx.Err() != nil || !errors.As(err, &change) || change.ID != "plex" {
				t.Fatalf("could not leave unfinished connection: %v (context: %v)", err, ctx.Err())
			}
			select {
			case <-connector.stopped:
			default:
				t.Fatal("pending connection was not stopped before switching")
			}
		})
	}
}

func TestSetupBackOffersOtherConnections(t *testing.T) {
	s := testSession(t)
	s.controller.running = false
	s.setup = connection.Presentation{Kind: connection.SetupFailure}
	s.about.Connections = []connection.Choice{{ID: "plex", Name: "Plex"}}
	s.handleKey(control.Back)
	if s.model.Quit || !s.about.Visible || !s.about.ConnectionsVisible {
		t.Fatal("Back did not offer another connection")
	}
	s.handleKey(control.Open)
	if s.connectionChange == nil || s.connectionChange.ID != "plex" {
		t.Fatal("could not choose a different connection after failure")
	}
}

func TestConnectionMenuBackReturnsToBrowsing(t *testing.T) {
	s := testSession(t)
	s.controller.running = false
	s.client = switchServer{id: "plex"}
	s.config.ConnectionID = "plex"
	s.model.Current().Selected = 3
	s.about.Connections = []connection.Choice{{Name: "Existing", Children: []connection.Choice{{ID: "plex", Name: "Plex"}}}}
	for _, key := range []control.Action{control.About, control.Down, control.Open, control.Back, control.Back, control.Back} {
		s.handleKey(key)
	}
	if s.about.Visible || s.model.Quit || s.connectionChange != nil || s.model.Current().Selected != 3 {
		t.Fatal("Back did not return to the unchanged browser")
	}
	for _, key := range []control.Action{control.About, control.Down, control.Open, control.Open} {
		s.handleKey(key)
	}
	if s.about.Visible || s.model.Quit || s.connectionChange != nil {
		t.Fatal("selecting the active account unnecessarily restarted it")
	}
}

func TestSetupBackCannotLoopThroughAbout(t *testing.T) {
	for _, previous := range []string{"", "plex"} {
		for _, viaAbout := range []bool{false, true} {
			t.Run(fmt.Sprintf("previous=%s/about=%v", previous, viaAbout), func(t *testing.T) {
				s := testSession(t)
				s.controller.running = false
				s.config.ReturnConnectionID = previous
				s.setup = connection.Presentation{Kind: connection.SetupServers}
				s.about.Connections = []connection.Choice{{ID: "jellyfin", Name: "Jellyfin"}}
				if viaAbout {
					s.handleKey(control.About)
					s.handleKey(control.Down)
				} else {
					s.handleKey(control.Back)
				}
				s.handleKey(control.Back)
				if !s.model.Quit {
					t.Fatal("Back returned to setup instead of leaving the attempt")
				}
				if previous == "" {
					if s.connectionChange != nil {
						t.Fatal("startup cancellation invented a previous connection")
					}
				} else if s.connectionChange == nil || s.connectionChange.ID != previous {
					t.Fatal("cancellation lost the previously connected account")
				}
			})
		}
	}
}

func TestConnectionReturnRouteSurvivesUnfinishedAttempts(t *testing.T) {
	s := testSession(t)
	s.controller.running = false
	s.client = switchServer{id: "plex"}
	s.config.ConnectionID = "plex"
	s.changeConnection("jellyfin-new")
	if s.connectionChange.ReturnID != "plex" {
		t.Fatal("working account not preserved")
	}
	pending := testSession(t)
	pending.controller.running = false
	pending.config.ReturnConnectionID = s.connectionChange.ReturnID
	pending.setup = connection.Presentation{Kind: connection.SetupApproval}
	pending.changeConnection("another-server")
	if pending.connectionChange.ReturnID != "plex" {
		t.Fatal("unfinished attempt replaced the return route")
	}
}

// TestCancelSetupRestoresRetainedBrowser runs a real browser loop through a
// working connection, an unfinished sign-in, and cancellation back to browsing.
func TestCancelSetupRestoresRetainedBrowser(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	keys := make(chan control.Event, 8)
	nav := &Navigation{}
	retained := &connection.Retained{Connector: switchConnector{server: switchServer{id: "plex"}}}
	run := func(cfg Config, actions []control.Action, ready func(rendering.Scene) bool) error {
		step := 0
		renderer := &controlCaptureRenderer{Renderer: rendering.NewRenderer(), capture: func(scene rendering.Scene) {
			if step < len(actions) && ready(scene) {
				keys <- control.Event{Action: actions[step]}
				step++
			}
		}}
		return Run(ctx, cfg, playback.Config{}, &runTestOutput{}, renderer, nil, keys)
	}
	cfg := Config{Connector: retained, ConnectionID: "plex", Navigation: nav, Connections: connectionChoices(connection.Choice{ID: "existing", Name: "Existing", Children: []connection.Choice{{ID: "plex", Name: "Plex"}}}, connection.Choice{ID: "jellyfin-new", Name: "Jellyfin"})}
	err := run(cfg, []control.Action{control.About, control.Down, control.Down, control.Open}, func(s rendering.Scene) bool { return s.Setup.Kind == rendering.SetupHidden })
	var change *connection.Change
	if !errors.As(err, &change) || change.ReturnID != "plex" {
		t.Fatalf("outbound handoff: %v", err)
	}
	blocked := blockedConnector{kind: connection.SetupApproval, stopped: make(chan struct{})}
	pending := Config{Connector: blocked, ConnectionID: "jellyfin", ReturnConnectionID: change.ReturnID, Connections: cfg.Connections}
	err = run(pending, []control.Action{control.Back, control.Back}, func(s rendering.Scene) bool { return s.Setup.Kind == connection.SetupApproval })
	if !errors.As(err, &change) || change.ID != "plex" {
		t.Fatalf("cancel handoff: %v", err)
	}
	select {
	case <-blocked.stopped:
	default:
		t.Fatal("unfinished sign-in did not stop")
	}
	if nav.model == nil {
		t.Fatal("previous browsing state was lost")
	}
	err = run(cfg, []control.Action{control.Quit}, func(s rendering.Scene) bool { return s.Setup.Kind == rendering.SetupHidden && !s.About.Visible })
	if err != nil || ctx.Err() != nil {
		t.Fatalf("return to browsing: %v, context %v", err, ctx.Err())
	}
}

// connectionChoices supplies an immutable fixture catalog.
func connectionChoices(choices ...connection.Choice) func() []connection.Choice {
	return func() []connection.Choice { return choices }
}

func TestAuthenticationRefreshesCatalogSnapshot(t *testing.T) {
	s := testSession(t)
	initial := []connection.Choice{{ID: "plex-new", Name: "Plex"}}
	choices := initial
	s.config.Connections = func() []connection.Choice { return choices }
	s.refreshConnections()
	choices = []connection.Choice{{ID: "existing", Name: "Existing", Children: []connection.Choice{{ID: "plex", Name: "Living room"}}}, initial[0]}
	s.handleAuth(authResult{generation: s.connection.generation, connection: &authenticatedConnection{client: switchServer{id: "plex"}}})
	if len(s.about.Connections) != 2 || s.about.Connections[0].Children[0].Name != "Living room" {
		t.Fatal("authenticated browser kept the startup catalog")
	}
}
