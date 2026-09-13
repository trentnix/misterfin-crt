//go:build linux

package evdev

import "testing"

func advertised(codes ...uint16) [96]byte {
	var keys [96]byte
	for _, code := range codes {
		keys[code/8] |= 1 << (code % 8)
	}
	return keys
}

func TestLabelsFollowEffectiveBindings(t *testing.T) {
	config := Config{Profiles: []Profile{{Match: "Pad", Buttons: map[uint16]string{310: "seek-backward", 311: "seek-forward", 305: "", 307: "open"}, ButtonLabels: map[uint16]string{310: "L1", 311: "R1", 307: "Square"}}}}
	d := device{name: "Pad", bindings: config.bindings("Pad"), triggers: map[uint16]*triggerAxis{2: {}, 5: {}}}
	labels := d.labels(advertised(304, 305, 307, 310, 311))
	if labels.Name("seek-backward") != "L1" || labels.Name("seek-forward") != "R1" || labels.Name("open") != "Square" || labels.Name("back") != "A" {
		t.Fatal(labels)
	}
	if labels.Name("track-previous") != "" || labels.Name("track-next") != "" {
		t.Fatal("removed track bindings advertised", labels)
	}
	// Moving an action to another button also moves its label. Unsupported
	// buttons must not supply instructions, even when a profile names them.
	d.bindings.Buttons[307] = "select"
	labels = d.labels(advertised(304, 305, 310, 311))
	if labels.Name("open") != "" || labels.Name("select") != "" {
		t.Fatal(labels)
	}
}

func TestLabelsRespectReplaceAndAxisNames(t *testing.T) {
	axis := Axis{Rest: "minimum", Positive: "seek-forward"}
	d := device{name: "Pad", bindings: Profile{Replace: true, Axes: map[uint16]Axis{4: axis}, AxisLabels: map[uint16]AxisLabels{4: {Positive: "R2"}}}, axes: map[uint16]*mappedAxis{4: newMappedAxis(axis, 0, 255)}, triggers: map[uint16]*triggerAxis{2: {}}}
	labels := d.labels(advertised(304, 305, 310, 311))
	if len(labels) != 1 || labels.Name("seek-forward") != "R2" {
		t.Fatal(labels)
	}
	d.bindings.Axes[4] = Axis{}
	if labels = d.labels(advertised(304)); len(labels) != 0 {
		t.Fatal("disabled axis advertised", labels)
	}
}

func TestLabelsUseKeyboardAndControllerNames(t *testing.T) {
	keyboard := device{name: "Keyboard"}
	labels := keyboard.labels(advertised(1, 26, 27, 28, 36, 38, 103))
	for action, want := range map[string]string{"open": "Enter", "back": "Esc", "track-previous": "[", "track-next": "]", "seek-backward": "J", "seek-forward": "L", "up": "Up"} {
		if labels.Name(action) != want {
			t.Errorf("%s: %s", action, labels.Name(action))
		}
	}
	pad := device{name: "Xbox", triggers: map[uint16]*triggerAxis{2: {}, 5: {}}, hats: [2]bool{true, true}}
	labels = pad.labels(advertised(304, 305, 310, 311, 314))
	for action, want := range map[string]string{"open": "B", "back": "A", "track-previous": "LB", "track-next": "RB", "seek-backward": "LT", "seek-forward": "RT", "up": "Up"} {
		if labels.Name(action) != want {
			t.Errorf("%s: %s", action, labels.Name(action))
		}
	}
}
