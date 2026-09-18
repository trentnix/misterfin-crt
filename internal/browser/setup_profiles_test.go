package browser

import (
	"fmt"
	"strings"
	"testing"

	"mistervision/internal/connection"
	"mistervision/internal/input/control"
	"mistervision/internal/rendering"
)

func TestProfilePINInputIsPrivateAndSubmittedOnce(t *testing.T) {
	s := testSession(t)
	s.controller.running = false
	choice := make(chan connection.ProfileSelection, 1)
	prompt := connection.ProfilePrompt{Profiles: []connection.Profile{{ID: "parent", Name: "Parent", Protected: true}, {ID: "child", Name: "Child"}}}
	if !(profileChoicesResult{prompt: prompt, choice: choice}).apply(s) {
		t.Fatal("profile prompt was not applied")
	}
	s.handleSetupKey(control.Open)
	if s.setup.Kind != connection.SetupPIN {
		t.Fatal("protected selection did not open keypad")
	}
	for _, key := range []control.Action{control.Open, control.Next, control.Open, control.Next, control.Open} {
		s.handleSetupKey(key)
	}
	if s.setup.PINLength != 3 || s.connection.profilePIN != "123" || strings.Contains(fmt.Sprint(s.setup), "123") {
		t.Fatal("PIN was not masked at the presentation boundary")
	}
	// Move from 3 down to 6, then left to 4.
	for _, key := range []control.Action{control.Down, control.Previous, control.Previous, control.Open} {
		s.handleSetupKey(key)
	}
	reply := <-choice
	if reply.ID != "parent" || reply.PIN != "1234" || s.connection.profilePIN != "" || s.connection.profileChoice != nil {
		t.Fatal("PIN reply was not consumed privately")
	}
	s.handleSetupKey(control.Open)
	if len(choice) != 0 {
		t.Fatal("selection was submitted twice")
	}
}

func TestProfileBackClearsPINAndRestoresConnection(t *testing.T) {
	s := testSession(t)
	s.controller.running = false
	s.config.ReturnConnectionID = "plex"
	choice := make(chan connection.ProfileSelection, 1)
	(profileChoicesResult{prompt: connection.ProfilePrompt{Profiles: []connection.Profile{{ID: "parent", Name: "Parent", Protected: true}}, PIN: true}, choice: choice}).apply(s)
	s.handleSetupKey(control.Open)
	s.handleSetupKey(control.Back)
	if s.setup.Kind != connection.SetupProfiles || s.connection.profilePIN != "" || s.setup.PINLength != 0 {
		t.Fatal("Back retained PIN input")
	}
	s.handleSetupKey(control.Back)
	if s.connectionChange == nil || s.connectionChange.ID != "plex" || !s.model.Quit {
		t.Fatal("cancel did not restore previous connection")
	}
	if len(choice) != 0 {
		t.Fatal("cancel submitted a profile")
	}
}

func TestProfileRetryAndStaleResult(t *testing.T) {
	s := testSession(t)
	s.connection.generation = 2
	p := connection.ProfilePrompt{Profiles: []connection.Profile{{ID: "parent", Name: "Parent", Protected: true}}, PIN: true, Message: "Incorrect PIN. Try again."}
	s.connection.profilePIN = "123"
	if (profileChoicesResult{generation: 1, prompt: p}).apply(s) {
		t.Fatal("stale profile prompt accepted")
	}
	if s.connection.profilePIN != "123" {
		t.Fatal("stale result changed input")
	}
	(profileChoicesResult{generation: 2, prompt: p, choice: make(chan connection.ProfileSelection, 1)}).apply(s)
	if s.connection.profilePIN != "" || s.setup.PINLength != 0 || s.setup.Kind != connection.SetupPIN || s.setup.Message == "" {
		t.Fatal("PIN retry lost its context")
	}
}

func TestAboutRequestsTentativeProfileSwitch(t *testing.T) {
	s := testSession(t)
	s.controller.running = false
	s.config.ConnectionID = "plex"
	s.client = switchServer{id: "plex"}
	s.about.Visible = true
	s.about.ProfileAction = connection.ProfileChoose
	s.setup.Kind = rendering.SetupHidden
	s.handleAboutKey(control.Up)
	if s.connectionChange == nil || s.connectionChange.ProfileAction == connection.ProfileUnchanged || s.connectionChange.ID != "plex" || s.connectionChange.ReturnID != "plex" {
		t.Fatal("About did not request a cancellable same-connection switch")
	}
}

func TestProfilePINErrorClearsOnFirstRetryDigit(t *testing.T) {
	s := testSession(t)
	s.controller.running = false
	choice := make(chan connection.ProfileSelection, 1)
	message := "Incorrect PIN. Try again."
	prompt := connection.ProfilePrompt{Profiles: []connection.Profile{{ID: "parent", Name: "Parent", Protected: true}}, PIN: true, Message: message}
	(profileChoicesResult{prompt: prompt, choice: choice}).apply(s)
	// Navigation and Delete do not acknowledge an error or start a new attempt.
	for _, action := range []control.Action{control.Down, control.Down, control.Down, control.Open, control.Next} {
		s.handleSetupKey(action)
		if s.setup.Message != message || s.setup.Kind != connection.SetupPIN || s.setup.PINLength != 0 {
			t.Fatalf("feedback changed before entering a digit after %v", action)
		}
	}
	// Zero is a digit too. Selecting it starts the retry and clears the message.
	s.handleSetupKey(control.Open)
	if s.setup.Message != "" || s.setup.PINLength != 1 || s.connection.profilePIN != "0" || len(choice) != 0 {
		t.Fatal("first retry digit did not clear feedback and preserve entry")
	}
	for i := 0; i < 3; i++ {
		s.handleSetupKey(control.Open)
	}
	if s.setup.Kind != connection.SetupPIN || !s.setup.PINChecking || len(choice) != 1 {
		t.Fatal("retry did not submit after four digits")
	}
	// Another rejection shows fresh feedback, and Back leaves it behind.
	(profileChoicesResult{prompt: prompt, choice: choice}).apply(s)
	if s.setup.Message != message || s.setup.PINLength != 0 {
		t.Fatal("new rejection did not restore feedback")
	}
	s.handleSetupKey(control.Back)
	if s.setup.Kind != connection.SetupProfiles || s.setup.Message != "" {
		t.Fatal("Back retained an error for a different profile selection")
	}
}

func TestProfileNavigationReachesAllProfiles(t *testing.T) {
	for _, count := range []int{4, 8} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			s := testSession(t)
			s.controller.running = false
			profiles := make([]connection.Profile, count)
			for i := range profiles {
				profiles[i] = connection.Profile{ID: fmt.Sprint(i), Name: fmt.Sprintf("Viewer %d", i+1)}
			}
			choice := make(chan connection.ProfileSelection, 1)
			(profileChoicesResult{prompt: connection.ProfilePrompt{Profiles: profiles}, choice: choice}).apply(s)
			s.handleSetupKey(control.Previous)
			if s.setup.Selected != 0 {
				t.Fatal("selection wrapped before first profile")
			}
			for i := 1; i < count; i++ {
				s.handleSetupKey(control.Next)
				if s.setup.Selected != i {
					t.Fatalf("could not reach profile %d", i)
				}
			}
			s.handleSetupKey(control.Next)
			if s.setup.Selected != count-1 {
				t.Fatal("selection wrapped past last profile")
			}
			s.handleSetupKey(control.Open)
			if reply := <-choice; reply.ID != profiles[count-1].ID {
				t.Fatal("selected the wrong profile after scrolling")
			}
		})
	}
}

func TestProfileBackThenDismissConnectionsRestoresPicker(t *testing.T) {
	s := testSession(t)
	s.controller.running = false
	s.about.Connections = []connection.Choice{{ID: "plex", Name: "Plex"}}
	choice := make(chan connection.ProfileSelection, 1)
	(profileChoicesResult{prompt: connection.ProfilePrompt{Profiles: []connection.Profile{{ID: "parent", Name: "Parent"}}}, choice: choice}).apply(s)
	s.dispatchKey(control.Back)
	if !s.about.ConnectionsVisible {
		t.Fatal("Back did not open Connections")
	}
	s.dispatchKey(control.About)
	if s.setup.Kind != connection.SetupProfiles || s.about.Visible || s.model.Quit {
		t.Fatal("dismissing Connections stranded the pending profile prompt")
	}
	s.dispatchKey(control.Open)
	if len(choice) != 1 {
		t.Fatal("restored picker could not submit a profile")
	}
}

func TestProfileKeypadMovesWithinRowsAndColumns(t *testing.T) {
	for _, test := range []struct {
		key    int
		action control.Action
		want   int
	}{
		{0, control.Previous, 0}, {2, control.Next, 2}, {3, control.Previous, 3}, {5, control.Next, 5},
		{6, control.Previous, 6}, {8, control.Next, 8}, {9, control.Previous, 9}, {11, control.Next, 11},
		{1, control.Up, 1}, {2, control.Up, 2}, {9, control.Down, 9}, {10, control.Down, 10},
		{0, control.Next, 1}, {2, control.Previous, 1}, {1, control.Down, 4}, {10, control.Up, 7},
	} {
		s := testSession(t)
		s.controller.running = false
		(profileChoicesResult{prompt: connection.ProfilePrompt{Profiles: []connection.Profile{{ID: "one", Name: "One", Protected: true}}, PIN: true}, choice: make(chan connection.ProfileSelection, 1)}).apply(s)
		s.setup.PINKey = test.key
		s.handleSetupKey(test.action)
		if s.setup.PINKey != test.want {
			t.Errorf("key %d with %s moved to %d, want %d", test.key, test.action, s.setup.PINKey, test.want)
		}
	}
}

func TestProfileAddUserUsesSeparateAction(t *testing.T) {
	s := testSession(t)
	s.controller.running = false
	choice := make(chan connection.ProfileSelection, 1)
	prompt := connection.ProfilePrompt{Profiles: []connection.Profile{{ID: "one", Name: "One"}}, AddUser: true}
	(profileChoicesResult{prompt: prompt, choice: choice}).apply(s)
	s.handleSetupKey(control.Select)
	reply := <-choice
	if reply.Action != connection.ProfileAdd || reply.ID != "" || reply.PIN != "" || s.connection.profileChoice != nil {
		t.Fatal("Add user submitted a saved identity")
	}
	s.handleSetupKey(control.Select)
	if len(choice) != 0 {
		t.Fatal("Add user was submitted twice")
	}
	// Plex does not offer this action. Select must leave its picker intact.
	prompt.AddUser = false
	(profileChoicesResult{prompt: prompt, choice: choice}).apply(s)
	s.handleSetupKey(control.Select)
	if len(choice) != 0 || s.setup.Kind != connection.SetupProfiles {
		t.Fatal("unoffered action changed profile selection")
	}
}

func TestProfileCancelReturnsToAbout(t *testing.T) {
	s := testSession(t)
	s.controller.running = false
	s.client = switchServer{id: "plex"}
	s.config.ConnectionID = "plex"
	s.config.Navigation = &Navigation{}
	s.setup.Kind = rendering.SetupHidden
	s.about.Visible = true
	s.about.ProfileAction = connection.ProfileChoose
	s.handleAboutKey(control.Up)
	s.rememberNavigation()
	pending := testSession(t)
	pending.config.ReturnConnectionID = "plex"
	(profileChoicesResult{prompt: connection.ProfilePrompt{Profiles: []connection.Profile{{ID: "viewer", Name: "Viewer"}}}}).apply(pending)
	pending.handleSetupKey(control.Back)
	if pending.connectionChange == nil || pending.connectionChange.ID != "plex" {
		t.Fatal("Back lost the working connection")
	}
	restored := testSession(t)
	restored.controller.running = false
	restored.client = s.client
	restored.config.Navigation = s.config.Navigation
	restored.restoreNavigation()
	if !restored.about.Visible {
		t.Fatal("Back from profiles did not return to About")
	}
	restored.handleAboutKey(control.Back)
	if restored.about.Visible || restored.model.Quit {
		t.Fatal("Back from About did not return to browsing")
	}
}

func TestQuickConnectBackRequestsProfiles(t *testing.T) {
	s := testSession(t)
	s.controller.running = false
	s.setup = connection.Presentation{Kind: connection.SetupApproval, Back: connection.BackProfiles}
	s.config.ReturnConnectionID = "jellyfin"
	t.Cleanup(s.connection.close)
	s.handleSetupKey(control.Back)
	if s.connection.profileAction != connection.ProfileChoose || s.connection.selectServer || s.connectionChange != nil || s.model.Quit {
		t.Fatal("Back escaped user switching instead of reopening profiles")
	}
}

func TestAddUserFailureBeforeCodeReturnsToPicker(t *testing.T) {
	for _, progress := range []bool{false, true} {
		s := testSession(t)
		s.controller.running = false
		s.config.ReturnConnectionID = "jellyfin"
		s.config.Connector = switchConnector{}
		choices := make(chan connection.ProfileSelection, 1)
		(profileChoicesResult{prompt: connection.ProfilePrompt{
			Profiles: []connection.Profile{{ID: "one", Name: "One"}, {ID: "two", Name: "Two"}}, AddUser: true,
		}, choice: choices}).apply(s)
		s.handleSetupKey(control.Select)
		if choice := <-choices; choice.Action != connection.ProfileAdd {
			t.Fatal("did not request Add user")
		}
		if progress {
			s.handleAuthCode(authCodeResult{presentation: connection.Presentation{Kind: connection.SetupConnecting}})
		}
		// No approval code arrives before initiation fails.
		s.handleAuth(authResult{err: fmt.Errorf("Quick Connect disabled")})
		if s.setup.Back != connection.BackProfiles {
			t.Fatal("progress or failure lost the preceding picker")
		}
		t.Cleanup(s.connection.close)
		s.handleSetupKey(control.Back)
		if s.connectionChange != nil || s.connection.profileAction != connection.ProfileChoose || s.model.Quit {
			t.Fatal("Back left the picker flow")
		}
	}
}

func TestSingleProfileAboutActionAndDirectSignInBack(t *testing.T) {
	for _, addUser := range []bool{false, true} {
		s := testSession(t)
		s.controller.running = false
		s.client = switchServer{id: "single"}
		s.config.ConnectionID = "single"
		s.setup.Kind = rendering.SetupHidden
		s.about.Visible = true
		if addUser {
			s.about.ProfileAction = connection.ProfileAdd
		}
		s.handleAboutKey(control.Up)
		if (s.connectionChange != nil) != addUser {
			t.Fatal("About dispatched an unavailable profile action")
		}
	}
	for _, failed := range []bool{false, true} {
		s := testSession(t)
		s.controller.running = false
		s.config.Connector = switchConnector{}
		s.config.ReturnConnectionID = "jellyfin"
		s.setup = connection.Presentation{Kind: connection.SetupApproval, Back: connection.BackConnection}
		if failed {
			s.handleAuth(authResult{err: fmt.Errorf("sign-in unavailable")})
		}
		s.handleSetupKey(control.Back)
		if s.connectionChange == nil || s.connectionChange.ID != "jellyfin" || s.connection.profileAction != connection.ProfileUnchanged || s.connection.selectServer {
			t.Fatal("direct sign-in Back did not restore the active connection")
		}
	}
}

func TestFailedProfileSelectionReturnsToActiveConnection(t *testing.T) {
	s := testSession(t)
	s.controller.running = false
	s.config.Connector = switchConnector{}
	s.config.ReturnConnectionID = "plex"
	choice := make(chan connection.ProfileSelection, 1)
	(profileChoicesResult{prompt: connection.ProfilePrompt{Profiles: []connection.Profile{{ID: "viewer", Name: "Viewer"}}}, choice: choice}).apply(s)
	s.handleSetupKey(control.Open)
	s.handleAuth(authResult{err: fmt.Errorf("server unavailable")})
	s.handleSetupKey(control.Back)
	if s.connectionChange == nil || s.connectionChange.ID != "plex" {
		t.Fatal("failed profile selection lost the active connection")
	}
}
