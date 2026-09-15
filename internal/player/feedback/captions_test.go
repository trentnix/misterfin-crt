package feedback

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
