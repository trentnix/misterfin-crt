package control

import (
	"fmt"
	"strings"
)

// Action identifies a semantic input independently of its physical binding.
// The zero value disables a binding. Repeated events retain the -repeat suffix.
// External bindings must be decoded with ParseBinding or UnmarshalText.
type Action string

const (
	// About opens or dismisses About when browsing.
	About Action = "about"
	// Up moves selection upward.
	Up Action = "up"
	// Down moves selection downward.
	Down Action = "down"
	// Previous moves left or to the previous browsing page.
	Previous Action = "previous"
	// Next moves right or to the next browsing page.
	Next Action = "next"
	// Open activates the current item or primary screen action.
	Open Action = "open"
	// Back returns, cancels, or stops according to the active screen.
	Back Action = "back"
	// Select invokes the secondary action shown by the current screen.
	Select Action = "select"
	// Retry retries the current request.
	Retry Action = "retry"
	// Quit requests application shutdown.
	Quit Action = "quit"
	// ToggleControls is synthesized from directions during playback.
	ToggleControls Action = "controls"
	// TrackPrevious selects the previous music track or browsing page.
	TrackPrevious Action = "track-previous"
	// TrackNext selects the next music track or browsing page.
	TrackNext Action = "track-next"
	// SeekBackward requests a backward seek step for recorded media.
	SeekBackward Action = "seek-backward"
	// SeekForward requests a forward seek step for recorded media.
	SeekForward Action = "seek-forward"
)

// ValidBinding reports whether an action can appear in user configuration.
// ToggleControls is synthesized during playback. Repeats come from input readers.
func (a Action) ValidBinding() bool {
	switch a {
	case "", About, Up, Down, Previous, Next, Open, Back, Select, Retry, Quit,
		TrackPrevious, TrackNext, SeekBackward, SeekForward:
		return true
	}
	return false
}

// ParseBinding accepts the documented action names and an empty disabled binding.
// It rejects misspellings, internal actions, and synthetic repeat suffixes.
func ParseBinding(text string) (Action, error) {
	action := Action(text)
	if !action.ValidBinding() {
		return "", fmt.Errorf("unknown input action %q", text)
	}
	return action, nil
}

// UnmarshalText validates action strings when decoding controller configuration.
// A failed decode leaves the destination unchanged.
func (a *Action) UnmarshalText(text []byte) error {
	parsed, err := ParseBinding(string(text))
	if err == nil {
		*a = parsed
	}
	return err
}

// Base returns the press action associated with a repeated input event.
func (a Action) Base() Action { return Action(strings.TrimSuffix(string(a), "-repeat")) }

// IsRepeat distinguishes held-input events from initial presses.
func (a Action) IsRepeat() bool { return strings.HasSuffix(string(a), "-repeat") }

// Repeat retains an action's identity while marking a held input. Empty actions
// remain empty. Applying Repeat again does not add another suffix.
func (a Action) Repeat() Action {
	if a == "" || a.IsRepeat() {
		return a
	}
	return a + "-repeat"
}
