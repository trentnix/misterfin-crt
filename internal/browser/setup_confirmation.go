package browser

import (
	"context"
	"errors"

	"mistervision/internal/connection"
	"mistervision/internal/input/control"
)

// askConfirmation waits for the event loop's one-use reply or cancellation.
func askConfirmation(ctx context.Context, generation int, prompt connection.Confirmation, send func(context.Context, workerResult)) (bool, error) {
	choice := make(chan bool, 1)
	send(ctx, confirmationResult{generation: generation, prompt: prompt, choice: choice})
	select {
	case accepted := <-choice:
		return accepted, nil
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

// forgetSignIn suspends remote playback requests while a local account decision
// is pending. Keeping the listener alive lets cancellation resume normal control.
func (s *browserSession) forgetSignIn() {
	s.remoteRequests.cancelAll()
	s.connection.forget(s.ctx, s.send)
}

// forgetResult returns to About unless removal committed. Only committed removal
// ends the active session. Providers supply safe text for local storage errors.
type forgetResult struct {
	generation int
	err        error
}

func (r forgetResult) apply(s *browserSession) bool {
	if !s.connection.current(r.generation) {
		return false
	}
	s.connection.forgetting = false
	s.connection.confirmation = nil
	if errors.Is(r.err, connection.ErrSignedOut) {
		return s.handleAuth(authResult{generation: r.generation, err: r.err})
	}
	s.setup = connection.Presentation{}
	s.about.Visible = true
	if r.err != nil && !errors.Is(r.err, connection.ErrCanceled) {
		p := s.config.Connector.Describe(r.err)
		s.about.Message = p.Title + ". " + p.Message
	}
	if err := s.pendingAuthError; err != nil {
		s.pendingAuthError = nil
		s.requireSignIn(err)
		s.about.Visible = false
	}
	return true
}

// confirmationResult keeps the decision on the event loop while the provider
// waits. A stale attempt cannot consume input intended for its replacement.
type confirmationResult struct {
	generation int
	prompt     connection.Confirmation
	choice     chan bool
}

func (r confirmationResult) apply(s *browserSession) bool {
	if !s.connection.current(r.generation) {
		return false
	}
	s.connection.confirmation = r.choice
	s.about.Visible = false
	s.setup = connection.Presentation{Kind: connection.SetupConfirm, Title: r.prompt.Title, Message: r.prompt.Message, Retry: r.prompt.Accept, Back: s.setup.Back}
	return true
}

// handleConfirmationKey accepts only Open or Back and submits a decision once.
func (s *browserSession) handleConfirmationKey(key control.Action) bool {
	if s.connection.confirmation == nil {
		return false
	}
	if key != control.Open && key != control.Back {
		return false
	}
	s.connection.confirmation <- key == control.Open
	s.connection.confirmation = nil
	if s.connection.forgetting {
		// Keep the confirmation visible while the provider finishes local work.
		return true
	}
	s.setup.Kind = connection.SetupConnecting
	s.setup.Title, s.setup.Message, s.setup.Retry = "Please wait", "", ""
	return true
}
