//go:build linux

package evdev

import (
	"testing"

	"misterfin-crt/internal/input/control"
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
