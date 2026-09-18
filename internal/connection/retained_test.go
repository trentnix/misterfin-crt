package connection

import (
	"context"
	"errors"
	"testing"

	"mistervision/internal/media"
)

type retainedTestServer struct{ media.Server }
type retainedTestConnector struct {
	calls int
	fail  bool
}

func (c *retainedTestConnector) Connect(context.Context, Interaction) (Session, error) {
	c.calls++
	if c.fail {
		return Session{}, errors.New("offline")
	}
	return Session{Server: &retainedTestServer{}, Endpoint: Server{ID: "test", Name: "Test", URL: "http://test"}}, nil
}
func (*retainedTestConnector) Describe(error) Presentation { return Presentation{Kind: SetupFailure} }

func TestRetainedAccountsAreIndependentAndCanReauthenticate(t *testing.T) {
	a, b := &retainedTestConnector{}, &retainedTestConnector{}
	saves := 0
	first := &Retained{Connector: a, Remember: func(endpoint Server) error {
		if endpoint != (Server{ID: "test", Name: "Test", URL: "http://test"}) {
			t.Fatal("retained account lost public endpoint metadata")
		}
		saves++
		return nil
	}}
	second := &Retained{Connector: b}
	one, err := first.Connect(t.Context(), Interaction{})
	if err != nil {
		t.Fatal(err)
	}
	two, err := second.Connect(t.Context(), Interaction{})
	if err != nil {
		t.Fatal(err)
	}
	again, err := first.Connect(t.Context(), Interaction{})
	if err != nil || one.Server != again.Server || one.Server == two.Server || a.calls != 1 || b.calls != 1 || saves != 2 {
		t.Fatal("switching discarded or mixed accounts")
	}
	again, err = first.Connect(t.Context(), Interaction{Reauthenticate: true})
	if err != nil || again.Server == one.Server || a.calls != 2 {
		t.Fatal("reauthentication reused rejected account")
	}
	_, err = first.Connect(t.Context(), Interaction{SelectServer: true})
	if err != nil || a.calls != 3 {
		t.Fatal("server selection reused cached account")
	}
	a.fail = true
	if _, err = first.Connect(t.Context(), Interaction{Reauthenticate: true}); err == nil || saves != 4 {
		t.Fatal("failed connection was remembered")
	}
	if got, err := second.Connect(t.Context(), Interaction{}); err != nil || got.Server != two.Server {
		t.Fatal("failed connection affected another account")
	}
}

func TestRetainedDoesNotRememberCanceledOrUnwritableSelections(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	calls := 0
	c := &Retained{Connector: &retainedTestConnector{}, Remember: func(Server) error { calls++; return errors.New("private disk detail") }}
	if _, err := c.Connect(ctx, Interaction{}); !errors.Is(err, context.Canceled) || calls != 0 {
		t.Fatal("canceled attempt saved state")
	}
	_, err := c.Connect(t.Context(), Interaction{})
	if err == nil || c.Describe(err).Title != "Can't remember connection" || c.Describe(err).Message == "private disk detail" {
		t.Fatal("unsafe or missing persistence failure")
	}
}

func TestFailedDiscoveryKeepsWorkingAccount(t *testing.T) {
	provider := &retainedTestConnector{}
	retained := &Retained{Connector: provider}
	original, err := retained.Connect(t.Context(), Interaction{})
	if err != nil {
		t.Fatal(err)
	}
	provider.fail = true
	tentative := retained.NewSelection()
	if _, err := tentative.Connect(t.Context(), Interaction{SelectServer: true}); err == nil {
		t.Fatal("expected discovery failure")
	}
	restored, err := retained.Connect(t.Context(), Interaction{})
	if err != nil || restored.Server != original.Server || provider.calls != 2 {
		t.Fatal("canceling discovery could not restore the working account")
	}
	if _, err := retained.Connect(t.Context(), Interaction{Reauthenticate: true}); err == nil {
		t.Fatal("expected authentication failure")
	}
	if _, err := retained.Connect(t.Context(), Interaction{}); err == nil {
		t.Fatal("rejected credentials were retained")
	}
}

func TestTentativeSelectionRetriesBeforeReplacingWorkingAccount(t *testing.T) {
	provider := &retainedTestConnector{}
	retained := &Retained{Connector: provider}
	original, err := retained.Connect(t.Context(), Interaction{})
	if err != nil {
		t.Fatal(err)
	}
	tentative := retained.NewSelection()
	provider.fail = true
	if _, err := tentative.Connect(t.Context(), Interaction{SelectServer: true}); err == nil {
		t.Fatal("expected failed sign-in")
	}
	// New code retries do not repeat discovery and must not reuse the old account.
	if _, err := tentative.Connect(t.Context(), Interaction{}); err == nil || provider.calls != 3 {
		t.Fatal("sign-in retry reused the working account")
	}
	restored, err := retained.Connect(t.Context(), Interaction{})
	if err != nil || restored.Server != original.Server {
		t.Fatal("tentative retry replaced the working account")
	}
	provider.fail = false
	replacement, err := tentative.Connect(t.Context(), Interaction{})
	if err != nil {
		t.Fatal(err)
	}
	restored, err = retained.Connect(t.Context(), Interaction{})
	if err != nil || restored.Server != replacement.Server || restored.Endpoint != replacement.Endpoint || restored.Server == original.Server || provider.calls != 4 {
		t.Fatal("successful setup did not replace the remembered account")
	}
	provider.fail = true
	if _, err := tentative.Connect(t.Context(), Interaction{Reauthenticate: true}); err == nil {
		t.Fatal("expected reauthentication failure")
	}
	if _, err := retained.Connect(t.Context(), Interaction{}); err == nil {
		t.Fatal("promoted route retained rejected credentials")
	}
}
