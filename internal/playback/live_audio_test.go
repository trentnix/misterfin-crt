package playback

import (
	"context"
	"mistervision/internal/media"
	"testing"
)

// liveAudioBackend isolates the shared preparation contract from provider HTTP.
type liveAudioBackend struct {
	media.Playback
	request media.LiveRequest
}

func (*liveAudioBackend) Identity() media.Identity {
	return media.Identity{Server: "test", User: "user"}
}
func (*liveAudioBackend) PlaybackDetails(context.Context, string) (media.Item, error) {
	return media.Item{ID: "channel", Type: "TvChannel"}, nil
}
func (b *liveAudioBackend) PrepareLive(_ context.Context, r media.LiveRequest) (media.PreparedStream, error) {
	b.request = r
	return media.PreparedStream{SourceID: "source", LiveAudio: true, Streams: []media.MediaStream{{Type: "Audio", Index: 7}}}, nil
}
func TestLivePreparationPreservesAudioChoice(t *testing.T) {
	for _, index := range []int{-1, 7} {
		backend := &liveAudioBackend{}
		choices := trackPreparation{livePicture: true}
		if index >= 0 {
			choices.explicit = &TrackOptions{Selection: media.TrackSelection{AudioIndex: index, SubtitleIndex: 9}, Picture: PictureZoom43}
		}
		offset := int64(90000000)
		session, err := preparePlayback(t.Context(), backend, Config{Timing: Timing{LiveFrameRate: 30000.0 / 1001}}, Request{Item: media.Item{ID: "channel", Type: "TvChannel"}, StartTicks: &offset}, choices)
		if err != nil {
			t.Fatal(err)
		}
		if backend.request.ChannelID != "channel" || backend.request.AudioIndex != index || backend.request.MaxFrameRate != 30000.0/1001 {
			t.Fatalf("live request: %+v", backend.request)
		}
		if !session.tracks.LiveAudio || session.tracks.Selection.AudioIndex != index || session.tracks.Selection.SubtitleIndex != -1 || session.start != 0 || session.state.CanSeek == nil || *session.state.CanSeek {
			t.Fatal("live metadata lost selection or allowed seeking")
		}
		if index >= 0 && session.tracks.Picture != PictureZoom43 {
			t.Fatal("live audio replacement lost picture mode")
		}
	}
}
