package evdev

import "testing"

func TestProfilesMergeAndReplace(t *testing.T) {
	c := Config{Profiles: []Profile{
		{Match: "*Controller*", Buttons: map[uint16]string{307: "select", 310: "seek-backward"}},
		{Match: "Custom Controller", Buttons: map[uint16]string{310: "", 311: "seek-forward"}},
		{Match: "Other Controller", Replace: true, Buttons: map[uint16]string{300: "open"}},
		{Match: "Other Controller", Buttons: map[uint16]string{301: "back"}},
	}}
	custom := c.bindings("Custom Controller")
	if custom.Replace || custom.Buttons[307] != "select" || custom.Buttons[311] != "seek-forward" {
		t.Fatal(custom)
	}
	if key, exists := custom.Buttons[310]; !exists || key != "" {
		t.Fatal("disabled binding lost")
	}
	other := c.bindings("Other Controller")
	if !other.Replace || len(other.Buttons) != 2 || other.Buttons[300] != "open" || other.Buttons[301] != "back" {
		t.Fatal(other)
	}
	if keyboard := c.bindings("Keyboard"); keyboard.Replace || len(keyboard.Buttons) != 0 {
		t.Fatal(keyboard)
	}
	// A reconnect resolves fresh maps rather than retaining held state or mutations.
	custom.Buttons[307] = "quit"
	if c.bindings("Custom Controller").Buttons[307] != "select" {
		t.Fatal("device mutated configuration")
	}
}
