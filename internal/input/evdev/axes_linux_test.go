//go:build linux

package evdev

import (
	"testing"
	"time"

	"mistervision/internal/input/control"
)

func TestConfiguredButtonsOverrideDefaultsAndRelease(t *testing.T) {
	for _, replace := range []bool{false, true} {
		d := device{name: "SFC30", held: make(map[uint16]control.Action), bindings: Profile{Replace: replace, Buttons: map[uint16]control.Action{304: control.TrackNext, 310: ""}}}
		if got := d.accept(event{Type: 1, Code: 304, Value: 1}); got != control.TrackNext {
			t.Fatal(got)
		}
		if got := d.accept(event{Type: 1, Code: 304, Value: 2}); got != "" || len(d.held) != 1 {
			t.Fatal("kernel repeat changed hold")
		}
		d.accept(event{Type: 1, Code: 304, Value: 0})
		if len(d.held) != 0 {
			t.Fatal("release stuck")
		}
		if got := d.accept(event{Type: 1, Code: 310, Value: 1}); got != "" {
			t.Fatal("disabled button fired")
		}
		got := d.accept(event{Type: 1, Code: 314, Value: 1})
		if (!replace && got != control.Select) || (replace && got != "") {
			t.Fatal("inheritance", got)
		}
	}
	d := device{name: "MiSTer virtual input", held: make(map[uint16]control.Action), bindings: Profile{Buttons: map[uint16]control.Action{305: control.Open}}}
	if got := d.accept(event{Type: 1, Code: 305, Value: 1}); got != "" {
		t.Fatal("synthetic action echo enabled")
	}
}

func TestConfiguredAxes(t *testing.T) {
	for _, tc := range []struct {
		binding  Axis
		min, max int32
		events   []int32
		want     []control.Action
	}{
		{Axis{Negative: control.SeekBackward, Positive: control.SeekForward}, -1000, 1000, []int32{0, 300, 240, 140, -300, -240, -140, 300}, []control.Action{"", control.SeekForward, control.SeekForward, "", control.SeekBackward, control.SeekBackward, "", control.SeekForward}},
		{Axis{Rest: "minimum", Positive: control.SeekForward}, 0, 1023, []int32{0, 300, 230, 100}, []control.Action{"", control.SeekForward, control.SeekForward, ""}},
		{Axis{Rest: "maximum", Negative: control.SeekBackward}, 0, 255, []int32{255, 170, 200, 230}, []control.Action{"", control.SeekBackward, control.SeekBackward, ""}},
		{Axis{Positive: control.Next, Negative: control.Previous}, -1, 1, []int32{-1, 0, 1, 0}, []control.Action{control.Previous, "", control.Next, ""}},
		{Axis{Rest: "minimum", Positive: control.Open, Press: 60, Release: 40}, -100, 100, []int32{0, 30, 0, -30}, []control.Action{"", control.Open, control.Open, ""}},
	} {
		a := newMappedAxis(tc.binding, tc.min, tc.max)
		for i, v := range tc.events {
			if got := a.action(v); got != tc.want[i] {
				t.Errorf("%+v at %d: %s, want %s", tc.binding, v, got, tc.want[i])
			}
		}
	}
	// Explicitly disabling a default trigger must beat its autodetected mapping.
	d := device{held: make(map[uint16]control.Action), triggers: map[uint16]*triggerAxis{2: {min: 0, max: 100}}, bindings: Profile{Axes: map[uint16]Axis{2: {}}}}
	if got := d.accept(event{Type: 3, Code: 2, Value: 100}); got != "" {
		t.Fatal("disabled axis fired", got)
	}
	binding := Axis{Rest: "minimum", Positive: control.TrackNext}
	d.bindings.Axes[2] = binding
	d.axes = map[uint16]*mappedAxis{2: newMappedAxis(binding, 0, 100)}
	if got := d.accept(event{Type: 3, Code: 2, Value: 50}); got != control.TrackNext {
		t.Fatal(got)
	}
	if got := d.accept(event{Type: 3, Code: 2, Value: 60}); got != "" {
		t.Fatal("motion repeated a press")
	}
	d.accept(event{Type: 3, Code: 2, Value: 0})
	if len(d.held) != 0 {
		t.Fatal("axis release stuck")
	}
	if got := d.accept(event{Type: 3, Code: 2, Value: 50}); got != control.TrackNext {
		t.Fatal("second press missing")
	}
}

// configuredTestAxes uses the same binding selection as device discovery, with
// driver ranges supplied by the test instead of requiring physical hardware.
func configuredTestAxes(name string, keys [96]byte, profile Profile, ranges map[uint16][2]int32) device {
	d := device{name: name, bindings: profile, held: make(map[uint16]control.Action), axes: make(map[uint16]*mappedAxis)}
	for code, binding := range d.axisBindings(keys) {
		if span, ok := ranges[code]; ok && span[1] > span[0] {
			d.axes[code] = newMappedAxis(binding, span[0], span[1])
		}
	}
	return d
}

func TestLeftStickDefaultsAndOverrides(t *testing.T) {
	for _, tc := range []struct {
		name    string
		keys    [96]byte
		profile Profile
		want    control.Action
	}{
		{"gamepad", advertised(304), Profile{}, control.Next},
		{"touchscreen", advertised(330), Profile{}, ""},
		{"mouse", advertised(272, 273), Profile{}, ""},
		{"keyboard", advertised(28, 103), Profile{}, ""},
		{"MiSTer virtual input", advertised(304), Profile{}, ""},
		{"replacement profile", advertised(304), Profile{Replace: true}, ""},
		{"disabled axis", advertised(304), Profile{Axes: map[uint16]Axis{0: {}}}, ""},
		{"remapped axis", advertised(304), Profile{Axes: map[uint16]Axis{0: {Positive: control.TrackNext}}}, control.TrackNext},
		{"explicit replacement", advertised(304), Profile{Replace: true, Axes: map[uint16]Axis{0: {Positive: control.Open}}}, control.Open},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := configuredTestAxes(tc.name, tc.keys, tc.profile, map[uint16][2]int32{0: {-1000, 1000}, 1: {-1000, 1000}, 3: {-1000, 1000}})
			if got := d.accept(event{Type: 3, Code: 0, Value: 500}); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
			if got := d.accept(event{Type: 3, Code: 3, Value: 1000}); got != "" {
				t.Fatal("right stick gained default binding", got)
			}
		})
	}
	d := configuredTestAxes("gamepad", advertised(304), Profile{}, map[uint16][2]int32{0: {0, 0}})
	if got := d.accept(event{Type: 3, Code: 0, Value: 1000}); got != "" {
		t.Fatal("unavailable axis fired", got)
	}
	if got := d.labels(advertised(304)).Name(control.Next); got != "" {
		t.Fatal("unavailable axis advertised", got)
	}
}

func TestLeftStickDeadZoneAndReversal(t *testing.T) {
	for _, span := range [][2]int32{{-32768, 32767}, {0, 255}, {-1000, 1000}} {
		for code, actions := range map[uint16][2]control.Action{0: {control.Previous, control.Next}, 1: {control.Up, control.Down}} {
			d := configuredTestAxes("gamepad", advertised(304), Profile{}, map[uint16][2]int32{code: span})
			rest := span[0] + (span[1]-span[0])/2
			for _, step := range []struct {
				percent int32
				want    control.Action
				held    bool
			}{
				{0, "", false}, {20, "", false}, {35, "", false}, {50, actions[1], true},
				{35, "", true}, {50, "", true}, {20, "", false},
				{-50, actions[0], true}, {50, actions[1], true}, {0, "", false},
			} {
				travel := span[1] - rest
				if step.percent < 0 {
					travel = rest - span[0]
				}
				value := rest + travel*step.percent/100
				got := d.accept(event{Type: 3, Code: code, Value: value})
				if got != step.want || (len(d.held) > 0) != step.held {
					t.Fatalf("range %v axis %d at %d%%: action %q held %v, want %q held %v", span, code, step.percent, got, d.held, step.want, step.held)
				}
			}
		}
	}
}

func TestStickAndDPadShareNavigationRepeat(t *testing.T) {
	d := configuredTestAxes("gamepad", advertised(304), Profile{}, map[uint16][2]int32{1: {-1000, 1000}})
	virtual := device{name: "MiSTer virtual input", held: make(map[uint16]control.Action)}
	var nav navigation
	start := time.Unix(0, 0)
	check := func(ms int, source *device, e event, want control.Action) {
		t.Helper()
		pressed := make(map[control.Action]bool)
		if source != nil {
			if key := source.accept(e); key != "" {
				pressed[key] = true
			}
		}
		held := make(map[control.Action]bool)
		for _, input := range []*device{&d, &virtual} {
			for _, key := range input.held {
				held[key] = true
			}
		}
		got := nav.update(held, pressed, start.Add(time.Duration(ms)*time.Millisecond))
		if want == "" && len(got) == 0 {
			return
		}
		if len(got) != 1 || got[0] != want {
			t.Fatalf("at %d ms: %v, want %q", ms, got, want)
		}
	}
	check(0, &d, event{Type: 3, Code: 1, Value: 500}, control.Down)
	check(10, &d, event{Type: 3, Code: 17, Value: 1}, "")
	check(20, &virtual, event{Type: 1, Code: 108, Value: 1}, "")
	check(30, &d, event{Type: 3, Code: 17, Value: 0}, "")
	check(40, &virtual, event{Type: 1, Code: 108, Value: 0}, "")
	for _, ms := range []int{350, 460, 570, 680, 790, 900, 1010, 1055} {
		check(ms-1, nil, event{}, "")
		check(ms, nil, event{}, control.Down.Repeat())
	}
	check(1060, &d, event{Type: 3, Code: 1, Value: 0}, "")
	check(1100, nil, event{}, "")
	check(1110, &d, event{Type: 3, Code: 1, Value: 500}, control.Down)
	check(1459, nil, event{}, "")
	check(1460, nil, event{}, control.Down.Repeat())
}
