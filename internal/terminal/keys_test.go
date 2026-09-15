package terminal

import (
	"reflect"
	"testing"
	"time"

	"misterfin-crt/internal/input/control"
)

func TestFragmentedInputAndEscape(t *testing.T) {
	d := Decoder{}
	now := time.Now()
	if got := d.Feed([]byte("\x1b["), now); len(got) != 0 {
		t.Fatal(got)
	}
	if got := d.Feed([]byte("B\x1b[6~bq"), now); !reflect.DeepEqual(got, []control.Action{control.Down, control.TrackNext, control.Open, control.Quit}) {
		t.Fatal(got)
	}
	if got := d.Feed([]byte{27}, now); len(got) != 0 {
		t.Fatal(got)
	}
	if got := d.Feed(nil, now.Add(60*time.Millisecond)); !reflect.DeepEqual(got, []control.Action{control.Back}) {
		t.Fatal(got)
	}
	if got := d.Feed([]byte("\x1b[1;2A"), now); len(got) != 0 {
		t.Fatal("unknown escape sequence leaked keys", got)
	}
}

func TestPlaybackBindingsAndEnhancedRepeatEvents(t *testing.T) {
	var d Decoder
	now := time.Now()
	got := d.Feed([]byte("jJlL[]\x1b[5~\x1b[6~"), now)
	want := []control.Action{control.SeekBackward, control.SeekBackward, control.SeekForward, control.SeekForward, control.TrackPrevious, control.TrackNext, control.TrackPrevious, control.TrackNext}
	if !reflect.DeepEqual(got, want) {
		t.Fatal(got)
	}
	got = d.Feed([]byte("\x1b[1;1A\x1b[1;1:2A\x1b[1;1:3A\x1b[1;1A\x1b[108;1:2u\x1b[93;1:2u\x1b[27u"), now)
	want = []control.Action{control.Up, "up-repeat", control.Up, "seek-forward-repeat", "track-next-repeat", control.Back}
	if !reflect.DeepEqual(got, want) {
		t.Fatal(got)
	}
	if got := d.Feed([]byte("\x1b[99;5u"), now); !reflect.DeepEqual(got, []control.Action{control.Quit}) {
		t.Fatal("Ctrl+C lost", got)
	}
}

func TestAboutKeyEncodings(t *testing.T) {
	for _, seq := range []string{"\x1bOP", "\x1b[11~", "\x1b[1P", "\x1b[57364u"} {
		var d Decoder
		got := d.Feed([]byte(seq), time.Now())
		if len(got) != 1 || got[0] != control.About {
			t.Fatalf("%q: %v", seq, got)
		}
	}
	if sequenceAction("57364;1:2u") != "about-repeat" || sequenceAction("57364;1:3u") != "" {
		t.Fatal("F1 repeat/release handling")
	}
}
