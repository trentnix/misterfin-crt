package browser

import (
	"context"
	"errors"
	"testing"
	"time"

	"mistervision/internal/connection"
	"mistervision/internal/input/control"
	"mistervision/internal/media"
	"mistervision/internal/remote"
)

func TestConfirmationRequiresExplicitOpenAndRepliesOnce(t *testing.T) {
	for _, key := range []control.Action{control.Open, control.Back} {
		s := testSession(t)
		s.controller.running = false
		s.connection.generation = 2
		choice := make(chan bool, 1)
		r := confirmationResult{generation: 1, prompt: connection.Confirmation{Title: "Forget user?", Message: "Remove this sign-in?", Accept: "Forget user"}, choice: choice}
		if r.apply(s) {
			t.Fatal("stale confirmation replaced current setup")
		}
		r.generation = 2
		s.setup.Back = connection.BackConnection
		r.apply(s)
		for _, ignored := range []control.Action{control.Up, control.Down, control.Select, control.Retry, control.Next} {
			s.handleSetupKey(ignored)
		}
		if len(choice) != 0 {
			t.Fatal("navigation accepted confirmation")
		}
		s.handleSetupKey(key)
		if accepted := <-choice; accepted != (key == control.Open) {
			t.Fatal("wrong confirmation reply")
		}
		s.handleSetupKey(control.Open)
		if len(choice) != 0 || s.connection.confirmation != nil || s.setup.Back != connection.BackConnection {
			t.Fatal("duplicate reply or lost return route")
		}
	}
}

// removalConnector waits at both local preparation and confirmation boundaries.
type removalConnector struct {
	prepare <-chan struct{}
	failure error
}

func (c removalConnector) Connect(ctx context.Context, i connection.Interaction) (connection.Session, error) {
	select {
	case <-c.prepare:
	case <-ctx.Done():
		return connection.Session{}, ctx.Err()
	}
	if i.ProfileAction != connection.ProfileForget {
		return connection.Session{}, errors.New("wrong removal action")
	}
	i.Show(connection.Presentation{Kind: connection.SetupConnecting})
	accepted, err := i.Ask(ctx, connection.Confirmation{Title: "Forget user?", Accept: "Forget user"})
	if err != nil {
		return connection.Session{}, err
	}
	if !accepted {
		return connection.Session{}, connection.ErrCanceled
	}
	if c.failure != nil {
		return connection.Session{}, c.failure
	}
	return connection.Session{}, connection.ErrSignedOut
}

func (removalConnector) Describe(error) connection.Presentation {
	return connection.Presentation{Kind: connection.SetupFailure, Title: "Cannot save sign-in", Message: "Check the sign-in folder."}
}

func TestAboutRemovalOpensConfirmationWithoutConnectionScreen(t *testing.T) {
	for _, outcome := range []string{"cancel", "forget", "storage failure"} {
		t.Run(outcome, func(t *testing.T) {
			s := testSession(t)
			t.Cleanup(s.connection.close)
			s.controller.running = false
			s.client = switchServer{id: "test"}
			s.config.ConnectionID = "test"
			prepare := make(chan struct{})
			provider := removalConnector{prepare: prepare}
			if outcome == "storage failure" {
				provider.failure = errors.New("private storage details")
			}
			s.config.Connector = provider
			s.connection.config = s.config
			s.about.Visible = true
			s.about.ForgetLabel = "Forget user"
			model, generation := s.model, s.connection.generation
			s.handleKey(control.Next)
			s.handleKey(control.Next)
			if !s.about.Visible || s.setup.Kind != connection.SetupHidden || s.connectionChange != nil || s.model.Quit || s.connection.generation != generation {
				t.Fatal("preparing removal left About or restarted the browser")
			}
			close(prepare)
			applyNext := func() {
				t.Helper()
				select {
				case r := <-s.events:
					r.apply(s)
				case <-time.After(2 * time.Second):
					t.Fatal("removal worker did not respond")
				}
			}
			applyNext()
			if s.about.Visible || s.setup.Kind != connection.SetupConfirm {
				t.Fatal("About did not transition directly to confirmation")
			}
			key := control.Open
			if outcome == "cancel" {
				key = control.Back
			}
			s.handleKey(key)
			s.handleKey(control.Open)
			if s.setup.Kind != connection.SetupConfirm {
				t.Fatal("reply flashed an intermediate screen")
			}
			applyNext()
			if outcome == "forget" {
				if s.client != nil || s.connectionChange == nil || !s.model.Quit {
					t.Fatal("confirmed removal retained the old sign-in")
				}
			} else {
				if !s.about.Visible || s.setup.Kind != connection.SetupHidden || s.client == nil || s.model != model || s.connectionChange != nil || s.connection.forgetting {
					t.Fatal("cancel or failure did not preserve About and browsing")
				}
				if outcome == "storage failure" && s.about.Message != "Cannot save sign-in. Check the sign-in folder." {
					t.Fatal("storage failure was hidden or exposed private details")
				}
			}
		})
	}
}

func TestAboutRemovalRequiresAvailableAction(t *testing.T) {
	s := testSession(t)
	s.controller.running = false
	s.client = switchServer{id: "test"}
	s.about.Visible = true
	s.handleAboutKey(control.Next)
	if s.connection.forgetting || s.connectionChange != nil || s.model.Quit {
		t.Fatal("unavailable removal was dispatched")
	}
}

func TestSignedOutResultCannotRestoreOldNavigation(t *testing.T) {
	for _, cleanupFailure := range []bool{false, true} {
		s := testSession(t)
		s.controller.running = false
		s.client = switchServer{id: "old"}
		s.config.ConnectionID = "test"
		s.config.ReturnConnectionID = "test"
		s.config.Connector = switchConnector{}
		s.config.Navigation = &Navigation{model: New()}
		s.about.CanReturnToConnection = true
		err := connection.ErrSignedOut
		if cleanupFailure {
			err = errors.Join(err, errors.New("storage failure"))
		}
		s.handleAuth(authResult{err: err})
		s.rememberNavigation()
		if s.client != nil || s.controlSource != nil || s.config.ReturnConnectionID != "" || s.about.CanReturnToConnection || s.config.Navigation.model != nil {
			t.Fatal("signed-out identity could be restored")
		}
		if cleanupFailure {
			if s.connectionChange != nil || s.setup.Back == connection.BackConnection {
				t.Fatal("cleanup error lost recovery or retained Back to old user")
			}
		} else if s.connectionChange == nil || s.connectionChange.ReturnID != "" || s.connectionChange.ID != "test" {
			t.Fatal("sign-out did not reopen fresh setup")
		}
	}
}

func TestConfirmationCannotBeCoveredByAbout(t *testing.T) {
	for _, removing := range []bool{false, true} {
		s := testSession(t)
		s.controller.running = false
		s.about.Visible = true
		s.connection.forgetting = removing
		choice := make(chan bool, 1)
		confirmationResult{prompt: connection.Confirmation{Title: "Confirm account change"}, choice: choice}.apply(s)
		for _, key := range []control.Action{control.About, control.About, control.Up, control.Next} {
			s.handleKey(key)
			if s.about.Visible || len(choice) != 0 || s.setup.Kind != connection.SetupConfirm {
				t.Fatal("global input covered or accepted the confirmation")
			}
		}
		s.handleKey(control.Back)
		if len(choice) != 1 || <-choice {
			t.Fatal("Back did not cancel the confirmation")
		}
	}
}

func TestRemovalBlocksRemotePlaybackAndRejectsPendingResults(t *testing.T) {
	s := testSession(t)
	t.Cleanup(s.connection.close)
	s.controller.running = false
	s.client = switchServer{id: "old"}
	s.about.Visible = true
	s.about.ForgetLabel = "Forget user"
	prepare := make(chan struct{})
	s.config.Connector = removalConnector{prepare: prepare}
	s.connection.config = s.config
	canceled := false
	s.remoteRequests.cancel = func() { canceled = true }
	s.remoteRequests.resolving = true
	late := remoteItemsResult{generation: s.remoteRequests.generation, items: []media.Item{{ID: "movie", Type: "Movie"}}}
	remoteGeneration := s.remote.generation
	s.handleKey(control.Next)
	if !canceled || s.remoteRequests.resolving {
		t.Fatal("removal did not cancel the pending media lookup")
	}
	play := remoteCommandResult{generation: remoteGeneration, command: remote.Command{Kind: remote.Play, IDs: []string{"movie"}}}
	if play.apply(s) || s.remoteRequests.resolving || late.apply(s) || s.controller.running {
		t.Fatal("remote playback interrupted confirmation preparation")
	}
	close(prepare)
	applyNext := func() {
		t.Helper()
		select {
		case r := <-s.events:
			r.apply(s)
		case <-time.After(2 * time.Second):
			t.Fatal("removal worker did not respond")
		}
	}
	applyNext()
	if play.apply(s) || late.apply(s) || s.controller.running {
		t.Fatal("remote playback interrupted the visible confirmation")
	}
	s.handleKey(control.Back)
	applyNext()
	if late.apply(s) || s.controller.running {
		t.Fatal("canceled media lookup resumed after canceling removal")
	}
	message := remoteCommandResult{generation: remoteGeneration, command: remote.Command{Kind: remote.Message, Text: "Still connected"}}
	if !message.apply(s) || s.message.Text != "Still connected" || s.remote.generation != remoteGeneration {
		t.Fatal("canceling removal did not restore remote control on the same session")
	}
}

// cleanupConnector models failures after credential removal has committed.
// Calls are serialized by connectionManager and no second confirmation is needed.
type cleanupConnector struct {
	actions chan connection.ProfileAction
	results []error
}

func (c *cleanupConnector) Connect(_ context.Context, i connection.Interaction) (connection.Session, error) {
	c.actions <- i.ProfileAction
	err := c.results[0]
	c.results = c.results[1:]
	return connection.Session{}, err
}

func (*cleanupConnector) Describe(error) connection.Presentation {
	return connection.Presentation{Kind: connection.SetupFailure, Title: connection.SignOutIncompleteTitle, Message: connection.SignOutIncompleteMessage, Retry: "Retry"}
}

func TestCleanupRetryFinishesRemovalBeforeSignIn(t *testing.T) {
	s := testSession(t)
	t.Cleanup(s.connection.close)
	s.controller.running = false
	s.client = switchServer{id: "old"}
	s.config.ConnectionID = "test"
	s.config.ReturnConnectionID = "test"
	failure := errors.Join(connection.ErrSignedOut, errors.New("cleanup failed"))
	provider := &cleanupConnector{actions: make(chan connection.ProfileAction, 1), results: []error{failure, errors.New("storage still unavailable"), connection.ErrSignedOut}}
	s.config.Connector = provider
	s.connection.config = s.config
	forgetResult{err: failure}.apply(s)
	for attempt := 0; attempt < 3; attempt++ {
		if s.client != nil || s.config.ReturnConnectionID != "" || s.connectionChange != nil || s.connection.profileAction != connection.ProfileForget {
			t.Fatal("incomplete removal retained credentials or lost its retry action")
		}
		s.handleKey(control.Open)
		if s.setup.Title != titleSignOutCleanup {
			t.Fatal("cleanup retry displayed authentication progress")
		}
		select {
		case action := <-provider.actions:
			if action != connection.ProfileForget {
				t.Fatalf("Retry requested %v instead of cleanup", action)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("cleanup did not start")
		}
		select {
		case r := <-s.events:
			r.apply(s)
		case <-time.After(2 * time.Second):
			t.Fatal("cleanup did not finish")
		}
	}
	if !s.model.Quit || s.connectionChange == nil || s.connectionChange.ID != "test" || s.connectionChange.ReturnID != "" {
		t.Fatal("finished cleanup did not open fresh sign-in")
	}
}

func TestBackgroundSignInFailureWaitsForRemovalDecision(t *testing.T) {
	for _, source := range []string{"page", "home", "selection"} {
		for _, phase := range []string{"preparing", "confirmation"} {
			for _, outcome := range []string{"cancel", "storage failure", "removed", "cleanup failure"} {
				t.Run(source+"/"+phase+"/"+outcome, func(t *testing.T) {
					s := testSession(t)
					s.controller.running = false
					s.client = switchServer{id: "old"}
					s.config.Connector = removalConnector{}
					s.connection.forgetting = true
					s.about.Visible = true
					request := s.model.Load(0)
					choice := make(chan bool, 1)
					confirmation := confirmationResult{prompt: connection.Confirmation{Title: "Forget user?"}, choice: choice}
					if phase == "confirmation" {
						confirmation.apply(s)
					}
					switch source {
					case "page":
						s.handlePage(pageResult{request: *request, err: media.ErrUnauthorized})
					case "home":
						s.handleHome(homeResult{err: media.ErrUnauthorized})
					case "selection":
						s.handleSelection(selectionResult{update: selectionUpdate{err: media.ErrUnauthorized}})
					}
					if !errors.Is(s.pendingAuthError, media.ErrUnauthorized) {
						t.Fatal("background failure was not retained")
					}
					if phase == "preparing" {
						if !s.about.Visible || s.setup.Kind != connection.SetupHidden {
							t.Fatal("background failure interrupted removal preparation")
						}
						confirmation.apply(s)
					}
					if s.about.Visible || s.setup.Kind != connection.SetupConfirm {
						t.Fatal("background failure replaced confirmation")
					}
					key, result := control.Back, connection.ErrCanceled
					if outcome != "cancel" {
						key = control.Open
						result = errors.New("storage failure")
					}
					committed := outcome == "removed" || outcome == "cleanup failure"
					if committed {
						result = connection.ErrSignedOut
						if outcome == "cleanup failure" {
							result = errors.Join(result, errors.New("cleanup failed"))
						}
					}
					s.handleKey(key)
					if len(choice) != 1 || (<-choice) != (key == control.Open) {
						t.Fatal("confirmation no longer accepted a decision")
					}
					// A second batch still belongs to the old account if it finishes
					// after the removal result, particularly during cleanup recovery.
					latePage := pageResult{request: *s.model.Load(0), err: media.ErrUnauthorized}
					lateHome := homeResult{generation: s.home.generation, err: media.ErrUnauthorized}
					lateSelection := selectionResult{generation: s.selection.generation, update: selectionUpdate{err: media.ErrUnauthorized}}
					forgetResult{err: result}.apply(s)
					if s.pendingAuthError != nil {
						t.Fatal("deferred failure survived its account decision")
					}
					if !committed {
						if s.about.Visible || s.setup.Kind != connection.SetupFailure || s.client == nil {
							t.Fatal("cancellation or storage failure lost sign-in recovery")
						}
						return
					}
					if s.client != nil || s.handlePage(latePage) || s.handleHome(lateHome) || s.handleSelection(lateSelection) || s.pendingAuthError != nil {
						t.Fatal("late work from the removed account changed recovery")
					}
					if outcome == "removed" {
						if !s.model.Quit || s.connectionChange == nil {
							t.Fatal("successful removal did not open fresh setup")
						}
					} else if s.model.Quit || s.connection.profileAction != connection.ProfileForget || s.setup.Kind != connection.SetupFailure {
						t.Fatal("background error replaced cleanup recovery")
					}
				})
			}
		}
	}
}
