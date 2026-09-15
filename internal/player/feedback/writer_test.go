package feedback

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"misterfin-crt/internal/player"
)

func TestWriterBoundsDiagnosticsAndResumesAtNextLine(t *testing.T) {
	var got []player.Feedback
	w := NewWriter(ParseANS, func(v player.Feedback) { got = append(got, v) }).(*writer)
	input := strings.Repeat("private diagnostic ", 10000)
	if n, err := w.Write([]byte(input)); err != nil || n != len(input) {
		t.Fatal(n, err)
	}
	if len(w.pending) > 8192 || len(got) != 0 {
		t.Fatal("unbounded or published diagnostic")
	}
	fmt.Fprint(w, "\rANS_TIME_POSITION=9\n")
	if len(got) != 1 || got[0].Position != 9 {
		t.Fatal("long output swallowed valid feedback")
	}
}

func TestWriterSerializesConcurrentFeedback(t *testing.T) {
	count := 0
	w := NewWriter(ParseANS, func(player.Feedback) { count++ })
	var writers sync.WaitGroup
	for range 8 {
		writers.Go(func() {
			for range 100 {
				fmt.Fprint(w, "ANS_TIME_POSITION=1\n")
			}
		})
	}
	writers.Wait()
	if count != 800 {
		t.Fatal("concurrent output lost or mixed complete lines", count)
	}
}
