package media

import "testing"

func TestSubtitleDeliveryPreservesCodecDefaults(t *testing.T) {
	for _, codec := range []string{"srt", "ass", "ssa", "subrip", "webvtt"} {
		stream := MediaStream{Type: "Subtitle", Codec: codec}
		if !stream.TextSubtitle() || !stream.ClientSubtitle() {
			t.Fatalf("default text handling changed for %s", codec)
		}
		stream.RequiresBurnIn = true
		if !stream.TextSubtitle() || stream.ClientSubtitle() {
			t.Fatalf("provider delivery requirement lost for %s", codec)
		}
	}
	if (MediaStream{Type: "Subtitle", Codec: "pgs"}).ClientSubtitle() {
		t.Fatal("image subtitle offered as text")
	}
}
