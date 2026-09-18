package browser

import (
	"context"
	"errors"
	"image"

	"mistervision/internal/connection"
	"mistervision/internal/media"
	"mistervision/internal/remote"
)

// authenticatedConnection groups the client and loaders that become valid
// together after authentication. The browser loop owns the returned values.
type authenticatedConnection struct {
	client        media.Server
	remote        remote.Source
	selection     *selectionLoader
	recovered     bool
	profile       *connection.Profile
	profileAction connection.ProfileAction
	forgetLabel   string
}

// serverChoice carries a selected server or an explicit rescan request.
type serverChoice struct {
	server connection.Server
	err    error
}

// connectionManager serializes connector attempts and owns their cancellation.
// Successful attempts assemble account-scoped loaders. Only the browser loop
// calls its methods.
type connectionManager struct {
	config         Config
	width, height  int
	cancel         context.CancelFunc
	generation     int
	reauthenticate bool
	newAccount     bool                     // Retained while requesting another approval code.
	profileAction  connection.ProfileAction // Explicit viewer action for this attempt.
	selectServer   bool                     // Keep discovery active across retries until a server is chosen.
	choice         chan serverChoice
	profileChoice  chan connection.ProfileSelection
	confirmation   chan bool
	forgetting     bool   // Local removal leaves the current browser intact until committed.
	profilePIN     string // Private, transient keypad input. Never copied into a scene.
	profileAvatars map[string]image.Image
	done           <-chan struct{} // Closes after this attempt and all preceding attempts exit.
}

func newConnectionManager(config Config, width, height int) connectionManager {
	return connectionManager{config: config, width: width, height: height, cancel: func() {}}
}

// connect replaces an earlier attempt and publishes progress and completion
// through the browser's worker-result boundary. Workers join their predecessor
// before touching session files, without blocking the browser loop.
func (m *connectionManager) connect(ctx context.Context, send func(context.Context, workerResult)) {
	m.cancel()
	m.choice = nil
	m.profileChoice = nil
	m.confirmation = nil
	m.forgetting = false
	m.profilePIN = ""
	m.profileAvatars = make(map[string]image.Image)
	m.generation++
	generation := m.generation
	work, cancel := context.WithCancel(ctx)
	m.cancel = cancel
	config, width, height, selectServer, reauthenticate := m.config, m.width, m.height, m.selectServer, m.reauthenticate
	newAccount, profileAction := m.newAccount, m.profileAction
	previous := m.done
	done := make(chan struct{})
	m.done = done
	go func() {
		defer close(done)
		if previous != nil {
			<-previous
		}
		if work.Err() != nil {
			return
		}
		artwork := profileArtworkWork{ctx: work, generation: generation, send: send, requested: make(map[string]bool)}
		defer artwork.wait()
		var session connection.Session
		var err error
		if config.Connector == nil {
			err = errors.New("no server connector configured")
		} else {
			session, err = config.Connector.Connect(work, connection.Interaction{
				SelectServer:   selectServer,
				ProfileAction:  profileAction,
				NewAccount:     newAccount,
				Reauthenticate: reauthenticate,
				ChooseProfile: func(ctx context.Context, prompt connection.ProfilePrompt) (connection.ProfileSelection, error) {
					choice := make(chan connection.ProfileSelection, 1)
					send(ctx, profileChoicesResult{generation: generation, prompt: prompt, choice: choice})
					artwork.start(prompt.Profiles, prompt.Avatars)
					select {
					case selected := <-choice:
						return selected, nil
					case <-ctx.Done():
						return connection.ProfileSelection{}, ctx.Err()
					}
				},
				Confirm: func(ctx context.Context, prompt connection.Confirmation) (bool, error) {
					return askConfirmation(ctx, generation, prompt, send)
				},
				Progress: func(p connection.Presentation) {
					send(work, authCodeResult{generation: generation, presentation: p})
				},
				ChooseServer: func(ctx context.Context, servers []connection.Server) (connection.Server, error) {
					choice := make(chan serverChoice, 1)
					send(ctx, serverChoicesResult{generation: generation, servers: append([]connection.Server(nil), servers...), choice: choice})
					select {
					case selected := <-choice:
						return selected.server, selected.err
					case <-ctx.Done():
						return connection.Server{}, ctx.Err()
					}
				},
			})
		}
		var connected *authenticatedConnection
		if err == nil {
			if session.Profile != nil {
				artwork.start([]connection.Profile{*session.Profile}, session.Avatars)
			}
			caches := newSelectionCaches(config, session.Server.Identity())
			selection := newSelectionLoader(session.Server, width, height, caches)
			selection.customBackground = config.Background != nil
			connected = &authenticatedConnection{client: session.Server, remote: session.Remote, recovered: session.Recovered, selection: selection, profile: session.Profile, profileAction: session.ProfileAction, forgetLabel: session.ForgetLabel}
		}
		send(work, authResult{generation: generation, connection: connected, err: err})
	}()
}

// forget prepares local removal without replacing the authenticated browser or
// its request generation. It joins earlier work before touching credentials,
// and close waits for removal just as it waits for a connection attempt.
func (m *connectionManager) forget(ctx context.Context, send func(context.Context, workerResult)) {
	m.forgetting = true
	m.cancel()
	ctx, cancel := context.WithCancel(ctx)
	m.cancel = cancel
	previous := m.done
	done := make(chan struct{})
	m.done = done
	generation, connector := m.generation, m.config.Connector
	go func() {
		defer close(done)
		if previous != nil {
			<-previous
		}
		if ctx.Err() != nil {
			return
		}
		_, err := connector.Connect(ctx, connection.Interaction{
			ProfileAction: connection.ProfileForget,
			Confirm: func(ctx context.Context, prompt connection.Confirmation) (bool, error) {
				return askConfirmation(ctx, generation, prompt, send)
			},
		})
		send(ctx, forgetResult{generation: generation, err: err})
	}()
}

func (m *connectionManager) current(generation int) bool {
	return generation == m.generation
}

func (m *connectionManager) close() {
	m.profilePIN = ""
	m.cancel()
	if m.done != nil {
		<-m.done
	}
}
