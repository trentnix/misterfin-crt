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

func TestNavigationAcceleratesAndResets(t *testing.T) {
	for _, key := range []string{"up", "down", "previous", "next"} {
		t.Run(key, func(t *testing.T) {
			var n navigation
			start := time.Unix(0, 0)
			held := map[string]bool{key: true}
			check := func(ms int, held, pressed map[string]bool, want string) {
				t.Helper()
				got := n.update(held, pressed, start.Add(time.Duration(ms)*time.Millisecond))
				if want == "" && len(got) == 0 {
					return
				}
				if len(got) != 1 || got[0] != want {
					t.Fatalf("at %d ms: got %v, want %q", ms, got, want)
				}
			}
			check(0, held, held, key)
			// These times match the C implementation, including the transition
			// from six slow intervals to the first fast interval.
			for _, ms := range []int{350, 460, 570, 680, 790, 900, 1010, 1055, 1100} {
				check(ms-1, held, nil, "")
				check(ms, held, nil, key+"-repeat")
			}
			// A delayed poll emits one repeat, then schedules from that poll.
			check(2000, held, nil, key+"-repeat")
			check(2001, held, nil, "")
			check(2045, held, nil, key+"-repeat")
			// Release (also used when a device disappears) resets acceleration.
			check(2050, nil, nil, "")
			check(2100, held, held, key)
			check(2449, held, nil, "")
			check(2450, held, nil, key+"-repeat")
			check(2495, held, nil, "")
			check(2560, held, nil, key+"-repeat")
			// Changing directions starts with a fresh press and delay.
			other := "up"
			if key == other {
				other = "down"
			}
			held = map[string]bool{other: true}
			check(2570, held, held, other)
			check(2919, held, nil, "")
			check(2920, held, nil, other+"-repeat")
		})
	}
}
