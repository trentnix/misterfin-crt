package playback

import (
	nativeplayer "misterfin-crt/internal/player/mplayer"
	"testing"
)

func TestCaptionUpdatesKeepLatestClearAcrossFragmentedWrites(t *testing.T) {
	ch := make(chan string, 1)
	p := playerProcess{captions: ch}
	w := nativeplayer.Decoder{}.Feedback(p.publishFeedback)
	for _, chunk := range []string{"ANS_CAP", "TION_TEXT=48656c", "6c6f\n", "ANS_CAPTION_TEXT=\n"} {
		w.Write([]byte(chunk))
	}
	if text := <-ch; text != "" {
		t.Fatal("clear lost behind old caption")
	}
	w.Write([]byte("ANS_CAPTION_TEXT=4e6577\nANS_CAPTION_TEXT=ff\n"))
	if text := <-ch; text != "New" {
		t.Fatal("malformed caption changed display")
	}
}
