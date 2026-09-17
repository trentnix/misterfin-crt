package browser

import (
	"context"
	"errors"

	"mistervision/internal/connection"
	"mistervision/internal/media"
	"mistervision/internal/remote"
)

// authenticatedConnection groups the client and loaders that become valid
// together after authentication. The browser loop owns the returned values.
type authenticatedConnection struct {
	client    media.Server
	remote    remote.Source
	selection *selectionLoader
	recovered bool
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
	newAccount     bool // Retained while requesting another approval code.
	selectServer   bool // Keep discovery active across retries until a server is chosen.
	choice         chan serverChoice
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
	m.generation++
	generation := m.generation
	work, cancel := context.WithCancel(ctx)
	m.cancel = cancel
	config, width, height, selectServer, reauthenticate := m.config, m.width, m.height, m.selectServer, m.reauthenticate
	newAccount := m.newAccount
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
		var session connection.Session
		var err error
		if config.Connector == nil {
			err = errors.New("no server connector configured")
		} else {
			session, err = config.Connector.Connect(work, connection.Interaction{
				SelectServer:   selectServer,
				NewAccount:     newAccount,
				Reauthenticate: reauthenticate,
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
		var connection *authenticatedConnection
		if err == nil {
			caches := newSelectionCaches(config, session.Server.Identity())
			selection := newSelectionLoader(session.Server, width, height, caches)
			selection.customBackground = config.Background != nil
			connection = &authenticatedConnection{client: session.Server, remote: session.Remote, recovered: session.Recovered, selection: selection}
		}
		send(work, authResult{generation: generation, connection: connection, err: err})
	}()
}

func (m *connectionManager) current(generation int) bool {
	return generation == m.generation
}

func (m *connectionManager) close() {
	m.cancel()
	if m.done != nil {
		<-m.done
	}
}
