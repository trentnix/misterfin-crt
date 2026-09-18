package browser

import (
	"context"
	"testing"
	"time"

	"mistervision/internal/media"
	"mistervision/internal/rendering"
)

func TestReauthenticationRejectsOldListings(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{{"page", nil}, {"authorization error", media.ErrUnauthorized}} {
		t.Run(tc.name, func(t *testing.T) {
			s := testSession(t)
			ctx, cancel := context.WithCancel(context.Background())
			s.ctx = ctx
			t.Cleanup(func() { cancel(); s.connection.close() })
			s.client = switchServer{id: "old"}
			s.config.Connector = switchConnector{server: switchServer{id: "new"}}
			s.connection.config = s.config
			old := pageResult{connectionGeneration: s.connection.generation, request: *s.model.Load(0), err: tc.err}
			s.authenticate()
			if s.handlePage(old) {
				t.Fatal("old listing changed setup while reauthenticating")
			}
			var auth authResult
			select {
			case result := <-s.events:
				var ok bool
				auth, ok = result.(authResult)
				if !ok || auth.err != nil {
					t.Fatalf("authentication failed: %T", result)
				}
			case <-time.After(time.Second):
				t.Fatal("authentication did not finish")
			}
			s.handleAuth(auth)
			if old.request.Generation != s.model.Generation {
				t.Fatal("fixture did not reproduce reused navigation generation")
			}
			if s.handlePage(old) || s.setup.Kind != rendering.SetupHidden {
				t.Fatal("old listing replaced authenticated screen")
			}
			s.home.loaded = true // Keep the empty current listing free of the Continue placeholder.
			for {
				select {
				case result := <-s.events:
					if page, ok := result.(pageResult); ok {
						if page.connectionGeneration != s.connection.generation || !s.handlePage(page) {
							t.Fatal("current connection's listing was rejected")
						}
						return
					}
				case <-time.After(time.Second):
					t.Fatal("current listing did not finish")
				}
			}
		})
	}
}
