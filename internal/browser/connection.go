package browser

import (
	"context"

	"misterfin-crt/internal/jellyfin"
)

// authenticatedConnection groups the client and loaders that become valid
// together after authentication. The browser loop owns the returned values.
type authenticatedConnection struct {
	client    *jellyfin.Client
	selection *selectionLoader
	recovered bool
}

// connectionManager owns configuration loading, authentication cancellation,
// and construction of dependencies scoped to the authenticated account. Only
// the browser loop calls its methods.
type connectionManager struct {
	config        Config
	width, height int
	cancel        context.CancelFunc
	generation    int
	done          <-chan struct{} // Closes after this attempt and all preceding attempts exit.
}

func newConnectionManager(config Config, width, height int) connectionManager {
	return connectionManager{config: config, width: width, height: height, cancel: func() {}}
}

// connect replaces an earlier attempt and publishes progress and completion
// through the browser's worker-result boundary. Workers join their predecessor
// before touching session files, without blocking the browser loop.
func (m *connectionManager) connect(ctx context.Context, send func(context.Context, workerResult)) {
	m.cancel()
	m.generation++
	generation := m.generation
	work, cancel := context.WithCancel(ctx)
	m.cancel = cancel
	config, width, height := m.config, m.width, m.height
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
		stage := connectionConfig
		server, err := jellyfin.LoadConfig(config.ConfigPath)
		var connection *authenticatedConnection
		if err == nil {
			stage = connectionSession
			var session jellyfin.Session
			var recovered bool
			session, recovered, err = jellyfin.LoadSession(config.StateDir, server.Server)
			if err == nil {
				stage = connectionAuthentication
				client := jellyfin.NewClient(server, session)
				client.Diagnostics = config.Diagnostics
				client.Version = config.Build.Version
				if recovered {
					config.Diagnostics.Record("authentication.session-recovered")
				}
				err = client.Authenticate(work, config.StateDir, func(code string) {
					send(work, authCodeResult{generation: generation, code: code, recovered: recovered})
				})
				if err == nil {
					caches := newSelectionCaches(config, client)
					selection := newSelectionLoader(client, width, height, caches)
					selection.customBackground = config.Background != nil
					connection = &authenticatedConnection{
						client:    client,
						recovered: recovered,
						selection: selection,
					}
				}
			}
		}
		send(work, authResult{generation: generation, connection: connection, stage: stage, err: err})
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
