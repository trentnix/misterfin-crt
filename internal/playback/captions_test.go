package playback

import (
	"encoding/hex"
	"strings"
	"testing"
)

func TestCaptionProtocol(t *testing.T) {
	for _, tc := range []struct{ prefix, text, want string }{
		{"ANS_CAPTION_TEXT=", "English\nEspañol", "English\nEspañol"},
		{"ANS_CAPTION_ASS=", `{\an7}{\pos(2,3)}Hello\N<i>world</i>`, "Hello\nworld"},
		{"ANS_CAPTION_TEXT=", "", ""},
		{"ANS_CAPTION_TEXT=", "hello\x1b\x00", "hello"},
	} {
		line := tc.prefix + hex.EncodeToString([]byte(tc.text))
		got, ok := parseCaption(line)
		if !ok || got != tc.want {
			t.Fatalf("caption: %q, %t", got, ok)
		}
	}
	for _, line := range []string{"ANS_CAPTION_TEXT=z0", "ANS_CAPTION_TEXT=0", "ANS_CAPTION_TEXT=ff", "ANS_CAPTION_TEXT=" + strings.Repeat("20", 2049), "unrelated=4849"} {
		if _, ok := parseCaption(line); ok {
			t.Fatal("invalid caption accepted")
		}
	}
}

func TestCaptionUpdatesKeepLatestClearAcrossFragmentedWrites(t *testing.T) {
	ch := make(chan string, 1)
	p := positionWriter{captions: ch}
	for _, chunk := range []string{"ANS_CAP", "TION_TEXT=48656c", "6c6f\n", "ANS_CAPTION_TEXT=\n"} {
		p.Write([]byte(chunk))
	}
	if text := <-ch; text != "" {
		t.Fatal("clear lost behind old caption")
	}
	p.Write([]byte("ANS_CAPTION_TEXT=4e6577\nANS_CAPTION_TEXT=ff\n"))
	if text := <-ch; text != "New" {
		t.Fatal("malformed caption changed display")
	}
}
