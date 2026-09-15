//go:build linux

package evdev

import (
	"testing"
	"time"

	"misterfin-crt/internal/input/control"
)

func TestControllerMatchesCMapping(t *testing.T) {
	for _, tc := range []struct {
		name string
		code uint16
		want control.Action
	}{
		{"Microsoft Xbox Controller", 314, control.Select},
		{"Microsoft Xbox Controller", 307, ""},
		{"Microsoft Xbox Controller", 305, control.Open},
		{"Microsoft Xbox Controller", 304, control.Back},
		{"Keyboard", 15, control.Select},
		{"Keyboard", 59, control.About},
		{"Microsoft Xbox Controller", 315, control.About},
		{"MiSTer virtual input", 15, ""},
		{"MiSTer virtual input", 28, ""},
		{"MiSTer virtual input", 103, control.Up},
		{"SFC30", 304, control.Open},
	} {
		if got := action(tc.name, 1, tc.code, 1); got != tc.want {
			t.Errorf("%s code %d: got %q, want %q", tc.name, tc.code, got, tc.want)
		}
	}
}

func TestReleaseAndKernelRepeat(t *testing.T) {
	d := device{name: "Xbox", held: make(map[uint16]control.Action)}
	if d.accept(event{Type: 3, Code: 17, Value: -1}) != control.Up {
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
	up := map[control.Action]bool{control.Up: true}
	if got := n.update(up, up, now); len(got) != 1 || got[0] != control.Up {
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
	if got := n.update(up, up, now.Add(420*time.Millisecond)); len(got) != 1 || got[0] != control.Up {
		t.Fatal(got)
	}
}

func TestNavigationAcceleratesAndResets(t *testing.T) {
	for _, key := range []control.Action{control.Up, control.Down, control.Previous, control.Next} {
		t.Run(string(key), func(t *testing.T) {
			var n navigation
			start := time.Unix(0, 0)
			held := map[control.Action]bool{key: true}
			check := func(ms int, held, pressed map[control.Action]bool, want control.Action) {
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
				check(ms, held, nil, key.Repeat())
			}
			// A delayed poll emits one repeat, then schedules from that poll.
			check(2000, held, nil, key.Repeat())
			check(2001, held, nil, "")
			check(2045, held, nil, key.Repeat())
			// Release (also used when a device disappears) resets acceleration.
			check(2050, nil, nil, "")
			check(2100, held, held, key)
			check(2449, held, nil, "")
			check(2450, held, nil, key.Repeat())
			check(2495, held, nil, "")
			check(2560, held, nil, key.Repeat())
			// Changing directions starts with a fresh press and delay.
			other := control.Up
			if key == other {
				other = control.Down
			}
			held = map[control.Action]bool{other: true}
			check(2570, held, held, other)
			check(2919, held, nil, "")
			check(2920, held, nil, other+"-repeat")
		})
	}
}

func TestPlaybackButtonsAndTriggerHysteresis(t *testing.T) {
	for code, want := range map[uint16]control.Action{310: control.TrackPrevious, 311: control.TrackNext, 312: control.SeekBackward, 313: control.SeekForward, 26: control.TrackPrevious, 27: control.TrackNext, 36: control.SeekBackward, 38: control.SeekForward} {
		if got := action("Xbox", 1, code, 1); got != want {
			t.Fatalf("code %d: %s", code, got)
		}
		if got := action("MiSTer virtual input", 1, code, 1); got != "" {
			t.Fatal("virtual action duplicated physical button")
		}
	}
	for _, bounds := range [][2]int32{{0, 255}, {0, 1023}, {-32768, 32767}} {
		axis := &triggerAxis{min: bounds[0], max: bounds[1]}
		d := device{name: "Xbox", held: make(map[uint16]control.Action), triggers: map[uint16]*triggerAxis{2: axis}}
		value := func(percent int32) int32 { return bounds[0] + (bounds[1]-bounds[0])*percent/100 }
		if d.accept(event{Type: 3, Code: 2, Value: value(10)}) != "" {
			t.Fatal("light touch triggered seek")
		}
		if d.accept(event{Type: 3, Code: 2, Value: value(30)}) != control.SeekBackward {
			t.Fatal("trigger press missing")
		}
		for _, percent := range []int32{24, 26, 20, 100} {
			if d.accept(event{Type: 3, Code: 2, Value: value(percent)}) != "" {
				t.Fatal("axis noise duplicated press")
			}
		}
		d.accept(event{Type: 3, Code: 2, Value: value(5)})
		if len(d.held) != 0 {
			t.Fatal("released trigger remained held")
		}
		if d.accept(event{Type: 3, Code: 2, Value: value(30)}) != control.SeekBackward {
			t.Fatal("second trigger press missing")
		}
	}
}

func TestSeekHoldRepeatsWithoutNavigationAcceleration(t *testing.T) {
	var n navigation
	now := time.Unix(0, 0)
	held := map[control.Action]bool{control.SeekForward: true}
	n.update(held, held, now)
	for _, ms := range []int{350, 600, 850, 1100, 1350, 1600, 1850, 2100, 2350} {
		if got := n.update(held, nil, now.Add(time.Duration(ms-1)*time.Millisecond)); len(got) != 0 {
			t.Fatal("seek accelerated")
		}
		if got := n.update(held, nil, now.Add(time.Duration(ms)*time.Millisecond)); len(got) != 1 || got[0] != "seek-forward-repeat" {
			t.Fatal(got)
		}
	}
	n.update(nil, nil, now.Add(3*time.Second))
	if got := n.update(nil, nil, now.Add(4*time.Second)); len(got) != 0 {
		t.Fatal("released trigger kept seeking")
	}
}
