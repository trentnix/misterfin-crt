package browser

import (
	"context"
	"image"
	"mistervision/internal/connection"
	"mistervision/internal/input/control"
	"testing"
	"time"
)

type profileTestConnector struct {
	connect func(context.Context, connection.Interaction) (connection.Session, error)
}

func (c profileTestConnector) Connect(ctx context.Context, i connection.Interaction) (connection.Session, error) {
	return c.connect(ctx, i)
}
func (c profileTestConnector) Describe(error) connection.Presentation {
	return connection.Presentation{Kind: connection.SetupConnecting, Title: "Connecting"}
}

type profileAvatarFunc func(context.Context, string) (image.Image, error)

func (f profileAvatarFunc) Load(ctx context.Context, id string) (image.Image, error) {
	return f(ctx, id)
}
func nextProfileResult(t *testing.T, s *browserSession) workerResult {
	t.Helper()
	select {
	case r := <-s.events:
		return r
	case <-time.After(2 * time.Second):
		t.Fatal("profile worker did not respond")
		return nil
	}
}

func TestProfileArtworkDoesNotDelaySelectionOrAuthentication(t *testing.T) {
	s := testSession(t)
	s.controller.running = false
	release := make(chan struct{})
	started := make(chan struct{}, 2)
	avatar := image.NewRGBA(image.Rect(0, 0, 2, 2))
	source := profileAvatarFunc(func(ctx context.Context, _ string) (image.Image, error) {
		started <- struct{}{}
		select {
		case <-release:
			return avatar, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})
	profiles := []connection.Profile{{ID: "one", Name: "One"}, {ID: "two", Name: "Two"}}
	config := Config{StateDir: t.TempDir(), Connector: profileTestConnector{connect: func(ctx context.Context, i connection.Interaction) (connection.Session, error) {
		_, err := i.ChooseProfile(ctx, connection.ProfilePrompt{Profiles: profiles, Avatars: source})
		if err != nil {
			return connection.Session{}, err
		}
		return connection.Session{Server: switchServer{id: "test"}, Profile: &profiles[0], Avatars: source}, nil
	}}}
	s.connection = newConnectionManager(config, 640, 240)
	defer s.connection.close()
	s.connection.connect(s.ctx, s.send)
	first := nextProfileResult(t, s)
	if _, ok := first.(profileChoicesResult); !ok {
		t.Fatal("profiles were not delivered first")
	}
	first.apply(s)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("avatar did not start")
	}
	s.handleSetupKey(control.Open)
	result, ok := nextProfileResult(t, s).(authResult)
	if !ok || result.err != nil || result.connection == nil {
		t.Fatal("authentication waited for artwork")
	}
	s.about.Profile = result.connection.profile
	close(release)
	for i := 0; i < 2; i++ {
		nextProfileResult(t, s).apply(s)
	}
	if s.about.Profile.Avatar != avatar {
		t.Fatal("late avatar did not reach the active viewer")
	}
	if profiles[0].Avatar != nil {
		t.Fatal("artwork mutated provider-owned profile data")
	}
}

func TestProfileArtworkPreservesInputAndRejectsStaleResults(t *testing.T) {
	s := testSession(t)
	s.controller.running = false
	s.connection.generation = 2
	profiles := []connection.Profile{{ID: "one", Name: "One", Protected: true}}
	choice := make(chan connection.ProfileSelection, 1)
	prompt := connection.ProfilePrompt{Profiles: profiles, PIN: true, Message: "Incorrect PIN. Try again."}
	(profileChoicesResult{generation: 2, prompt: prompt, choice: choice}).apply(s)
	s.setup.PINKey = 7
	s.setup.PINLength = 1
	s.connection.profilePIN = "1"
	avatar := image.NewRGBA(image.Rect(0, 0, 2, 2))
	if (profileAvatarResult{generation: 1, id: "one", avatar: avatar}).apply(s) {
		t.Fatal("stale avatar accepted")
	}
	(profileAvatarResult{generation: 2, id: "one", avatar: avatar}).apply(s)
	if s.setup.PINKey != 7 || s.setup.PINLength != 1 || s.connection.profilePIN != "1" || s.setup.Message != prompt.Message || s.connection.profileChoice != choice {
		t.Fatal("artwork arrival changed keypad state")
	}
	if s.setup.Profiles[0].Avatar != avatar || profiles[0].Avatar != nil {
		t.Fatal("artwork snapshot ownership failed")
	}
	(profileChoicesResult{generation: 2, prompt: prompt, choice: choice}).apply(s)
	if s.setup.Profiles[0].Avatar != avatar {
		t.Fatal("retry lost previously loaded artwork")
	}
}

func TestProfileArtworkCancellationJoinsWorkers(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	started := make(chan string, 8)
	source := profileAvatarFunc(func(ctx context.Context, id string) (image.Image, error) {
		started <- id
		<-ctx.Done()
		return nil, ctx.Err()
	})
	w := profileArtworkWork{ctx: ctx, requested: make(map[string]bool), send: func(context.Context, workerResult) { t.Error("canceled artwork published a result") }}
	profiles := []connection.Profile{{ID: "1"}, {ID: "2"}, {ID: "3"}, {ID: "4"}, {ID: "5"}, {ID: "6"}, {ID: "7"}, {ID: "8"}}
	w.start(profiles, source)
	w.start(profiles, source)
	for i := 0; i < 4; i++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("avatar workers did not start")
		}
	}
	select {
	case <-started:
		t.Fatal("more than four avatars loaded concurrently")
	default:
	}
	cancel()
	done := make(chan struct{})
	go func() { w.wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("canceled artwork workers did not exit")
	}
}

func TestBackCancelsPINVerificationAndReopensProfiles(t *testing.T) {
	s := testSession(t)
	s.controller.running = false
	canceled := make(chan struct{})
	calls := 0
	profiles := []connection.Profile{{ID: "one", Name: "One", Protected: true}}
	config := Config{StateDir: t.TempDir(), Connector: profileTestConnector{connect: func(ctx context.Context, i connection.Interaction) (connection.Session, error) {
		calls++
		if calls == 2 && !i.SelectProfile {
			t.Error("cancel did not request profile selection")
		}
		_, err := i.ChooseProfile(ctx, connection.ProfilePrompt{Profiles: profiles, PIN: !i.SelectProfile})
		if calls == 1 {
			defer close(canceled)
			if err != nil {
				return connection.Session{}, err
			}
			<-ctx.Done()
		}
		return connection.Session{}, ctx.Err()
	}}}
	s.config = config
	s.connection = newConnectionManager(config, 640, 240)
	defer s.connection.close()
	s.authenticate()
	nextProfileResult(t, s).apply(s)
	for i := 0; i < 4; i++ {
		s.handleSetupKey(control.Open)
	}
	if s.setup.Kind != connection.SetupPIN || !s.setup.PINChecking || s.setup.PINLength != 4 || s.connection.profilePIN != "" {
		t.Fatal("verification left the masked keypad")
	}
	key := s.setup.PINKey
	s.handleSetupKey(control.Next)
	s.handleSetupKey(control.Open)
	if s.setup.PINKey != key || s.connection.profileChoice != nil {
		t.Fatal("verification accepted duplicate input")
	}
	s.handleSetupKey(control.Back)
	// A canceled worker can queue an old result. Generation checks must discard it.
	for s.setup.Kind != connection.SetupProfiles {
		nextProfileResult(t, s).apply(s)
	}
	select {
	case <-canceled:
	default:
		t.Fatal("previous verification was not canceled")
	}
	if s.setup.PINChecking || s.setup.PINLength != 0 {
		t.Fatal("Back retained verification input")
	}
}
