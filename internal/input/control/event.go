// Package control defines input actions and the labels shown for their bindings.
package control

// Event carries an action and the immutable binding labels of its input device.
// A repeat retains its -repeat suffix. Labels must not change after publication.
type Event struct {
	Action string
	Labels Labels
}

// Labels maps semantic actions to short physical input names. An empty map
// means no bindings. A nil map uses the default controller layout in previews.
// Readers publish one map per device, reused by every event and rendered frame.
type Labels map[string]string

// Name returns the key or button bound to an action. An empty name means that
// the action has no binding on this device and must not appear in its legend.
func (l Labels) Name(action string) string {
	if l == nil {
		l = controllerLabels
	}
	return l[action]
}

var controllerLabels = Labels{
	"up": "Up", "down": "Down", "previous": "Left", "next": "Right",
	"open": "B", "back": "A", "select": "View", "retry": "R", "quit": "Q",
	"track-previous": "LB", "track-next": "RB", "seek-backward": "LT", "seek-forward": "RT",
}

var keyboardLabels = Labels{
	"up": "Up", "down": "Down", "previous": "Left", "next": "Right",
	"open": "Enter", "back": "Esc", "select": "Tab", "retry": "R", "quit": "Q",
	"track-previous": "[", "track-next": "]", "seek-backward": "J", "seek-forward": "L",
}

// KeyboardLabels returns the terminal's immutable primary bindings. Alternate
// keys still work but do not compete for space in the on-screen instructions.
func KeyboardLabels() Labels { return keyboardLabels }
