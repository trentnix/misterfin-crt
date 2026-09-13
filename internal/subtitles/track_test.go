package subtitles

import "testing"

func TestTimedTextBoundariesMarkupAndOverlap(t *testing.T) {
	track, err := Parse([]byte("\ufeff1\r\n00:00:01,000 --> 00:00:03,000\r\n{\\an8}<i>café</i> &amp; 5 < 6\\Nsecond line\r\n\r\n2\n00:00:02,000 --> 00:00:04,000\n<font color=\"red\">overlap</font>\n"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		ticks int64
		want  string
	}{{9999999, ""}, {10000000, "café & 5 < 6\nsecond line"}, {20000000, "café & 5 < 6\nsecond line\noverlap"}, {30000000, "overlap"}, {40000000, ""}, {10000000, "café & 5 < 6\nsecond line"}} {
		if got := track.At(tc.ticks); got != tc.want {
			t.Fatalf("at %d got %q want %q", tc.ticks, got, tc.want)
		}
	}
}
func TestMalformedSubtitlesFail(t *testing.T) {
	for _, input := range []string{"not srt", "1\n00:99:00,000 --> 01:00:00,000\nInvalid", "1\n00:00:02,000 --> 00:00:01,000\nReversed"} {
		if _, err := Parse([]byte(input)); err == nil {
			t.Fatal("accepted malformed cues")
		}
	}
}
