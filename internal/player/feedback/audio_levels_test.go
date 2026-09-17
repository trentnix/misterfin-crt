package feedback

import (
	"testing"

	"mistervision/internal/player"
)

func TestParseAudioLevels(t *testing.T) {
	for _, line := range []string{"ANS_AUDIO_LEVELS=NaN,0", "ANS_AUDIO_LEVELS=Inf,0", "ANS_AUDIO_LEVELS=-1,0", "ANS_AUDIO_LEVELS=0,2", "ANS_AUDIO_LEVELS=0"} {
		if _, ok := parseAudioLevels(line); ok {
			t.Fatalf("accepted %q", line)
		}
	}
	if levels, ok := parseAudioLevels("ANS_AUDIO_LEVELS=0.5,0.1"); !ok || levels != (player.AudioLevels{.5, .1}) {
		t.Fatal(levels, ok)
	}
}
