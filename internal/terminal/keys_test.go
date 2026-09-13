package terminal

import (
	"reflect"
	"testing"
	"time"
)

func TestFragmentedInputAndEscape(t *testing.T) {
	d := Decoder{}
	now := time.Now()
	if got := d.Feed([]byte("\x1b["), now); len(got) != 0 {
		t.Fatal(got)
	}
	if got := d.Feed([]byte("B\x1b[6~bq"), now); !reflect.DeepEqual(got, []string{"down", "track-next", "open", "quit"}) {
		t.Fatal(got)
	}
	if got := d.Feed([]byte{27}, now); len(got) != 0 {
		t.Fatal(got)
	}
	if got := d.Feed(nil, now.Add(60*time.Millisecond)); !reflect.DeepEqual(got, []string{"back"}) {
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
	want := []string{"seek-backward", "seek-backward", "seek-forward", "seek-forward", "track-previous", "track-next", "track-previous", "track-next"}
	if !reflect.DeepEqual(got, want) {
		t.Fatal(got)
	}
	got = d.Feed([]byte("\x1b[1;1A\x1b[1;1:2A\x1b[1;1:3A\x1b[1;1A\x1b[108;1:2u\x1b[93;1:2u\x1b[27u"), now)
	want = []string{"up", "up-repeat", "up", "seek-forward-repeat", "track-next-repeat", "back"}
	if !reflect.DeepEqual(got, want) {
		t.Fatal(got)
	}
	if got := d.Feed([]byte("\x1b[99;5u"), now); !reflect.DeepEqual(got, []string{"quit"}) {
		t.Fatal("Ctrl+C lost", got)
	}
}
