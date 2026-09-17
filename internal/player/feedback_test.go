package player_test

import (
	"fmt"
	"reflect"
	"testing"

	"mistervision/internal/player"
	"mistervision/internal/player/ffplay"
	"mistervision/internal/player/mplayer"
	"mistervision/internal/player/pythonhelper"
)

func TestDecodersParseOnlyTheirOwnFeedback(t *testing.T) {
	for _, tc := range []struct {
		decoder  player.Decoder
		wantsANS bool
	}{
		{mplayer.Decoder{}, true}, {pythonhelper.Decoder{}, true}, {ffplay.Decoder{}, false},
	} {
		t.Run(tc.decoder.Name(), func(t *testing.T) {
			var got []player.Feedback
			w := tc.decoder.Feedback(func(value player.Feedback) { got = append(got, value) })
			chunks := []string{"private diagnostic and URL\rANS_TIME_POS", "ITION=1.25\r\n2.50 M-V: 0.001\r", "3.5 A-V: 0\r4.5 M-A: 0\n", "ANS_TIME_POSITION=NaN\nANS_TIME_POSITION=-1\nANS_TIME_POSITION=1000000000\n", "NaN M-V: 0\rInf M-V: 0\r-1 A-V: 0\r1000000000 M-A: 0\r", "ANS_AUDIO_LEVELS=0.5,0.1\nANS_VIDEO_STARTED=true\nANS_BUFFERING=false\n", "ANS_PICTURE_MODE=7,1\nANS_CAPTION_TEXT=4869\nANS_CAPTION_TEXT=\n"}
			for _, chunk := range chunks {
				if n, err := w.Write([]byte(chunk)); err != nil || n != len(chunk) {
					t.Fatalf("write %d: %v", n, err)
				}
			}
			var want []player.Feedback
			if tc.wantsANS {
				want = []player.Feedback{
					{Kind: player.FeedbackPosition, Position: 1.25},
					{Kind: player.FeedbackLevels, Levels: player.AudioLevels{.5, .1}},
					{Kind: player.FeedbackVideoStarted},
					{Kind: player.FeedbackBuffering, Buffering: false},
					{Kind: player.FeedbackPicture, Picture: player.PictureResult{Request: 7, Mode: player.PictureZoom43}},
					{Kind: player.FeedbackCaption, Caption: "Hi"},
					{Kind: player.FeedbackCaption, Caption: ""},
				}
			} else {
				for _, seconds := range []float64{2.5, 3.5, 4.5} {
					want = append(want, player.Feedback{Kind: player.FeedbackPosition, Position: seconds})
				}
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("feedback = %+v, want %+v", got, want)
			}
		})
	}
}

func TestDecoderWritersDoNotSharePartialLines(t *testing.T) {
	for _, d := range []player.Decoder{mplayer.Decoder{}, pythonhelper.Decoder{}, ffplay.Decoder{}} {
		t.Run(d.Name(), func(t *testing.T) {
			prefix, suffix := "ANS_TIME_", "POSITION=1\n"
			if d.Name() == "ffplay" {
				prefix, suffix = "1 M-", "V: 0\r"
			}
			count := 0
			emit := func(player.Feedback) { count++ }
			first, second := d.Feedback(emit), d.Feedback(emit)
			fmt.Fprint(first, prefix)
			fmt.Fprint(second, suffix)
			if count != 0 {
				t.Fatal("a second process inherited pending output")
			}
			fmt.Fprint(first, suffix)
			if count != 1 {
				t.Fatal("first process lost its partial line")
			}
		})
	}
}

func TestPictureSettingsAreIndependentCopies(t *testing.T) {
	for _, d := range []player.Decoder{mplayer.Decoder{Width: 640, Height: 480}, pythonhelper.Decoder{Script: "video.py", Output: "frame", Width: 640, Height: 240}, ffplay.Decoder{}} {
		t.Run(d.Name(), func(t *testing.T) {
			original := d.WithPicture(player.PictureOriginal)
			zoom := d.WithPicture(player.PictureZoom43)
			if reflect.DeepEqual(original, zoom) || !reflect.DeepEqual(d, original) {
				t.Fatal("picture selection changed shared decoder settings")
			}
			if !reflect.DeepEqual(d.WithPicture(player.PictureOriginal), original) {
				t.Fatal("next request inherited zoom")
			}
		})
	}
}
