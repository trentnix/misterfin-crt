//go:build linux

package evdev

import (
	"testing"

	"mistervision/internal/input/control"
)

func advertised(codes ...uint16) [96]byte {
	var keys [96]byte
	for _, code := range codes {
		keys[code/8] |= 1 << (code % 8)
	}
	return keys
}

func TestLabelsFollowEffectiveBindings(t *testing.T) {
	config := Config{Profiles: []Profile{{Match: "Pad", Buttons: map[uint16]control.Action{310: control.SeekBackward, 311: control.SeekForward, 305: "", 307: control.Open}, ButtonLabels: map[uint16]string{310: "L1", 311: "R1", 307: "Square"}}}}
	d := device{name: "Pad", bindings: config.bindings("Pad"), triggers: map[uint16]*triggerAxis{2: {}, 5: {}}}
	labels := d.labels(advertised(304, 305, 307, 310, 311))
	if labels.Name(control.SeekBackward) != "L1" || labels.Name(control.SeekForward) != "R1" || labels.Name(control.Open) != "Square" || labels.Name(control.Back) != "A" {
		t.Fatal(labels)
	}
	if labels.Name(control.TrackPrevious) != "" || labels.Name(control.TrackNext) != "" {
		t.Fatal("removed track bindings advertised", labels)
	}
	// Moving an action to another button also moves its label. Unsupported
	// buttons must not supply instructions, even when a profile names them.
	d.bindings.Buttons[307] = control.Select
	labels = d.labels(advertised(304, 305, 310, 311))
	if labels.Name(control.Open) != "" || labels.Name(control.Select) != "" {
		t.Fatal(labels)
	}
}

func TestLabelsRespectReplaceAndAxisNames(t *testing.T) {
	axis := Axis{Rest: "minimum", Positive: control.SeekForward}
	d := device{name: "Pad", bindings: Profile{Replace: true, Axes: map[uint16]Axis{4: axis}, AxisLabels: map[uint16]AxisLabels{4: {Positive: "R2"}}}, axes: map[uint16]*mappedAxis{4: newMappedAxis(axis, 0, 255)}, triggers: map[uint16]*triggerAxis{2: {}}}
	labels := d.labels(advertised(304, 305, 310, 311))
	if len(labels) != 1 || labels.Name(control.SeekForward) != "R2" {
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
	for action, want := range map[control.Action]string{control.Open: "Enter", control.Back: "Esc", control.TrackPrevious: "[", control.TrackNext: "]", control.SeekBackward: "J", control.SeekForward: "L", control.Up: "Up"} {
		if labels.Name(action) != want {
			t.Errorf("%s: %s", action, labels.Name(action))
		}
	}
	pad := device{name: "Xbox", triggers: map[uint16]*triggerAxis{2: {}, 5: {}}, hats: [2]bool{true, true}}
	labels = pad.labels(advertised(304, 305, 310, 311, 314, 315))
	for action, want := range map[control.Action]string{control.Open: "B", control.Back: "A", control.Select: "Select", control.About: "Start", control.TrackPrevious: "LB", control.TrackNext: "RB", control.SeekBackward: "LT", control.SeekForward: "RT", control.Up: "Up"} {
		if labels.Name(action) != want {
			t.Errorf("%s: %s", action, labels.Name(action))
		}
	}
}

func TestLeftStickLabelsAndExplicitPreference(t *testing.T) {
	ranges := map[uint16][2]int32{0: {-1000, 1000}, 1: {-1000, 1000}}
	d := configuredTestAxes("gamepad", advertised(304), Profile{}, ranges)
	labels := d.labels(advertised(304))
	for action, name := range map[control.Action]string{control.Previous: "Left", control.Next: "Right", control.Up: "Up", control.Down: "Down"} {
		if labels.Name(action) != name {
			t.Fatalf("%s: %q, want %q", action, labels.Name(action), name)
		}
	}
	profile := Profile{Buttons: map[uint16]control.Action{311: control.Down}, ButtonLabels: map[uint16]string{311: "R1"}, Axes: map[uint16]Axis{0: {}}}
	d = configuredTestAxes("gamepad", advertised(304, 311), profile, ranges)
	labels = d.labels(advertised(304, 311))
	if labels.Name(control.Down) != "R1" {
		t.Fatal("stick displaced explicit label", labels)
	}
	if labels.Name(control.Next) != "" || labels.Name(control.Previous) != "" {
		t.Fatal("disabled stick advertised", labels)
	}
}

func TestExplicitFaceButtonsKeepTheirActionsAndLabels(t *testing.T) {
	for _, replace := range []bool{false, true} {
		config := Config{Profiles: []Profile{{Match: "*Xbox*", Replace: replace, Buttons: map[uint16]control.Action{304: control.Open, 305: control.Back}}}}
		d := device{name: "Xbox Controller", held: make(map[uint16]control.Action), bindings: config.bindings("Xbox Controller")}
		labels := d.labels(advertised(304, 305))
		for _, tc := range []struct {
			code   uint16
			action control.Action
			label  string
		}{{304, control.Open, "A"}, {305, control.Back, "B"}} {
			if got := d.accept(event{Type: 1, Code: tc.code, Value: 1}); got != tc.action {
				t.Fatalf("replace=%v code=%d: action %q, want %q", replace, tc.code, got, tc.action)
			}
			if got := labels.Name(tc.action); got != tc.label {
				t.Fatalf("replace=%v action=%s: label %q, want %q", replace, tc.action, got, tc.label)
			}
			d.accept(event{Type: 1, Code: tc.code, Value: 0})
		}
	}
}
