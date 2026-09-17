package playback

import (
	"fmt"
	"testing"

	"mistervision/internal/player"
	"mistervision/internal/player/mplayer"
)

func TestFullFeedbackQueuesDoNotBlockAndKeepLatestState(t *testing.T) {
	p := playerProcess{positions: make(chan float64, 1), levels: make(chan AudioLevels, 1), buffering: make(chan bool, 1), videoStarted: make(chan struct{}, 1), pictures: make(chan PictureResult, 1), captions: make(chan string, 1)}
	w := mplayer.Decoder{}.Feedback(p.publishFeedback)
	// Feed a burst without a playback consumer. Every write must finish, even
	// when measurements and notifications have already filled their queues.
	for i := 1; i <= 100; i++ {
		fmt.Fprintf(w, "ANS_TIME_POSITION=%d\nANS_AUDIO_LEVELS=0.5,0.1\nANS_BUFFERING=true\nANS_VIDEO_STARTED=true\nANS_PICTURE_MODE=%d,1\nANS_CAPTION_TEXT=4869\n", i, i)
	}
	fmt.Fprint(w, "ANS_PICTURE_MODE=101,-1\nANS_CAPTION_TEXT=\n")
	if <-p.positions != 1 || <-p.levels != (AudioLevels{.5, .1}) || !<-p.buffering || len(p.videoStarted) != 1 {
		t.Fatal("measurement queue policy changed")
	}
	reply := <-p.pictures
	if reply.Request != 101 || reply.Err == nil {
		t.Fatal("latest picture acknowledgment was lost", reply)
	}
	if <-p.captions != "" {
		t.Fatal("caption clear was lost behind old text")
	}
	// Missing optional callbacks also cannot block output processing.
	empty := playerProcess{}
	for _, kind := range []player.FeedbackKind{player.FeedbackPosition, player.FeedbackLevels, player.FeedbackBuffering, player.FeedbackVideoStarted, player.FeedbackPicture, player.FeedbackCaption} {
		empty.publishFeedback(player.Feedback{Kind: kind})
	}
}
