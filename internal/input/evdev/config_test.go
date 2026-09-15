package evdev

import (
	"testing"

	"misterfin-crt/internal/input/control"
)

func TestProfilesMergeAndReplace(t *testing.T) {
	c := Config{Profiles: []Profile{
		{Match: "*Controller*", Buttons: map[uint16]control.Action{307: control.Select, 310: control.SeekBackward}},
		{Match: "Custom Controller", Buttons: map[uint16]control.Action{310: "", 311: control.SeekForward}},
		{Match: "Other Controller", Replace: true, Buttons: map[uint16]control.Action{300: control.Open}},
		{Match: "Other Controller", Buttons: map[uint16]control.Action{301: control.Back}},
	}}
	custom := c.bindings("Custom Controller")
	if custom.Replace || custom.Buttons[307] != control.Select || custom.Buttons[311] != control.SeekForward {
		t.Fatal(custom)
	}
	if key, exists := custom.Buttons[310]; !exists || key != "" {
		t.Fatal("disabled binding lost")
	}
	other := c.bindings("Other Controller")
	if !other.Replace || len(other.Buttons) != 2 || other.Buttons[300] != control.Open || other.Buttons[301] != control.Back {
		t.Fatal(other)
	}
	if keyboard := c.bindings("Keyboard"); keyboard.Replace || len(keyboard.Buttons) != 0 {
		t.Fatal(keyboard)
	}
	// A reconnect resolves fresh maps rather than retaining held state or mutations.
	custom.Buttons[307] = control.Quit
	if c.bindings("Custom Controller").Buttons[307] != control.Select {
		t.Fatal("device mutated configuration")
	}
}

// Callers constructing Config directly must obey the same binding rules as JSON.
func TestProgrammaticConfigRejectsInternalActions(t *testing.T) {
	for _, action := range []control.Action{control.ToggleControls, control.Up.Repeat(), control.Action("typo")} {
		config := Config{Profiles: []Profile{{Match: "*", Buttons: map[uint16]control.Action{1: action}}}}
		if config.Validate() == nil {
			t.Fatal("invalid binding accepted", action)
		}
	}
}
