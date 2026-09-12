//go:build linux

package evdev

import (
	"testing"
	"time"
)

func TestControllerMatchesCMapping(t *testing.T) {
	for _, tc := range []struct {
		name string
		code uint16
		want string
	}{
		{"Microsoft Xbox Controller", 314, "select"},
		{"Microsoft Xbox Controller", 307, ""},
		{"Microsoft Xbox Controller", 305, "open"},
		{"Microsoft Xbox Controller", 304, "back"},
		{"Keyboard", 15, "select"},
		{"MiSTer virtual input", 15, ""},
		{"MiSTer virtual input", 28, ""},
		{"MiSTer virtual input", 103, "up"},
		{"SFC30", 304, "open"},
	} {
		if got := action(tc.name, 1, tc.code, 1); got != tc.want {
			t.Errorf("%s code %d: got %q, want %q", tc.name, tc.code, got, tc.want)
		}
	}
}

func TestReleaseAndKernelRepeat(t *testing.T) {
	d := device{name: "Xbox", held: make(map[uint16]string)}
	if d.accept(event{Type: 3, Code: 17, Value: -1}) != "up" {
		t.Fatal("hat up missing")
	}
	d.accept(event{Type: 3, Code: 17, Value: 0})
	if len(d.held) != 0 {
		t.Fatal("hat release remained held")
	}
	d.accept(event{Type: 1, Code: 103, Value: 1})
	if d.accept(event{Type: 1, Code: 103, Value: 2}) != "" || len(d.held) != 1 {
		t.Fatal("kernel repeat changed held state")
	}
	d.accept(event{Type: 1, Code: 103, Value: 0})
	if len(d.held) != 0 {
		t.Fatal("key release remained held")
	}
}

func TestNavigationMergesEchoAndRepeats(t *testing.T) {
	var n navigation
	now := time.Unix(0, 0)
	up := map[string]bool{"up": true}
	if got := n.update(up, up, now); len(got) != 1 || got[0] != "up" {
		t.Fatal(got)
	}
	// A delayed virtual arrow must not turn one physical press into two toggles.
	if got := n.update(up, up, now.Add(20*time.Millisecond)); len(got) != 0 {
		t.Fatal(got)
	}
	if got := n.update(up, nil, now.Add(400*time.Millisecond)); len(got) != 1 || got[0] != "up-repeat" {
		t.Fatal(got)
	}
	n.update(nil, nil, now.Add(410*time.Millisecond))
	if got := n.update(up, up, now.Add(420*time.Millisecond)); len(got) != 1 || got[0] != "up" {
		t.Fatal(got)
	}
}
