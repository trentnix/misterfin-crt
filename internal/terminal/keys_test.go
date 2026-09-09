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
	if got := d.Feed([]byte("B\x1b[6~bq"), now); !reflect.DeepEqual(got, []string{"down", "next", "open", "quit"}) {
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
