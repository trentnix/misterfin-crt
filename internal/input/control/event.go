// Package control defines input actions and the labels shown for their bindings.
package control

// Event carries an action and the immutable binding labels of its input device.
// A repeat retains its -repeat suffix. Labels must not change after publication.
type Event struct {
	Action Action
	Labels Labels
}

// Labels maps semantic actions to short physical input names. An empty map
// means no bindings. A nil map uses the default controller layout in previews.
// Readers publish one map per device, reused by every event and rendered frame.
type Labels map[Action]string

// Name returns the key or button bound to an action. An empty name means that
// the action has no binding on this device and must not appear in its legend.
func (l Labels) Name(action Action) string {
	if l == nil {
		l = controllerLabels
	}
	return l[action]
}

var controllerLabels = Labels{
	Up: "Up", Down: "Down", Previous: "Left", Next: "Right",
	About: "Start", Open: "B", Back: "A", Select: "Select", Retry: "R", Quit: "Q",
	TrackPrevious: "LB", TrackNext: "RB", SeekBackward: "LT", SeekForward: "RT",
}

var keyboardLabels = Labels{
	Up: "Up", Down: "Down", Previous: "Left", Next: "Right",
	About: "F1", Open: "Enter", Back: "Esc", Select: "Tab", Retry: "R", Quit: "Q",
	TrackPrevious: "[", TrackNext: "]", SeekBackward: "J", SeekForward: "L",
}

// KeyboardLabels returns the terminal's immutable primary bindings. Alternate
// keys still work but do not compete for space in the on-screen instructions.
func KeyboardLabels() Labels { return keyboardLabels }
