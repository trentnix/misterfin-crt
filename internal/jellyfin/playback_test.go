package jellyfin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"misterfin-crt/internal/media"
)

func TestVideoStreamQueryMatchesC(t *testing.T) {
	c := NewClient(Config{Server: "http://example.test/jellyfin"}, Session{Token: "private & token", DeviceID: "device"})
	for _, ntsc := range []bool{false, true} {
		u, err := url.Parse(prepareTestVideo(t, c, media.VideoRequest{Item: Item{ID: "movie"}, SessionID: "session", StartTicks: 12345678, NTSC: ntsc, Tracks: media.TrackSelection{AudioIndex: -1}, BurnSubtitle: -1}).URL)
		if err != nil {
			t.Fatal(err)
		}
		fps := "25"
		if ntsc {
			fps = "30"
		}
		want := url.Values{"subtitleStreamIndex": {"-1"}, "static": {"false"}, "videoCodec": {"mpeg2video"}, "container": {"ts"}, "audioCodec": {"mp3"}, "audioChannels": {"2"}, "allowVideoStreamCopy": {"false"}, "audioSampleRate": {"48000"}, "maxWidth": {"720"}, "maxHeight": {"576"}, "videoBitRate": {"12000000"}, "maxFramerate": {fps}, "startTimeTicks": {"12345678"}, "playSessionId": {"session"}, "deviceId": {"device"}, "ApiKey": {"private & token"}}
		if u.Path != "/jellyfin/Videos/movie/stream" || !reflect.DeepEqual(u.Query(), want) {
			t.Fatal("stream query differs from C")
		}
	}

}

func TestAudioStreamQueryMatchesC(t *testing.T) {
	c := NewClient(Config{Server: "https://server/jellyfin"}, Session{Token: "private & token"})
	u, err := url.Parse(prepareTestAudio(t, c).URL)
	if err != nil {
		t.Fatal(err)
	}
	if u.Path != "/jellyfin/Audio/track/stream" || !reflect.DeepEqual(u.Query(), url.Values{"static": {"true"}, "playSessionId": {"session"}, "ApiKey": {"private & token"}}) {
		t.Fatal("audio query differs from C")
	}
}

// prepareTestVideo exercises the consumer contract, including preparation errors.
func prepareTestVideo(t *testing.T, c *Client, request media.VideoRequest) media.PreparedStream {
	t.Helper()
	stream, err := c.PrepareVideo(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	return stream
}

func prepareTestAudio(t *testing.T, c *Client) media.PreparedStream {
	t.Helper()
	stream, err := c.PrepareAudio(t.Context(), Item{ID: "track"}, "session")
	if err != nil {
		t.Fatal(err)
	}
	return stream
}

func TestSelectedVideoUsesActualStreamIndexes(t *testing.T) {
	c := NewClient(Config{Server: "http://server"}, Session{Token: "secret"})
	for _, burn := range []int{-1, 12} {
		raw := prepareTestVideo(t, c, media.VideoRequest{Item: Item{ID: "item"}, SessionID: "session", StartTicks: 900000000, NTSC: true, SourceID: "source-id", Tracks: media.TrackSelection{AudioIndex: 7, SubtitleIndex: 12}, BurnSubtitle: burn}).URL
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		q := u.Query()
		if q.Get("audioStreamIndex") != "7" || q.Get("mediaSourceId") != "source-id" || q.Get("startTimeTicks") != "900000000" {
			t.Fatal("lost stream selection or seek position")
		}
		if burn >= 0 {
			if q.Get("subtitleStreamIndex") != "12" || q.Get("subtitleMethod") != "Encode" {
				t.Fatal("missing burn-in")
			}
		} else if q.Get("subtitleStreamIndex") != "-1" || q.Has("subtitleMethod") {
			t.Fatal("implicit subtitles enabled")
		}
	}
}

func TestSubtitleUsesAuthenticatedExtractionEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/Videos/item/source/Subtitles/12/Stream.srt" || !strings.Contains(r.Header.Get("Authorization"), `Token="secret"`) {
			t.Error("wrong source/index or missing authorization")
		}
		w.Write([]byte("1\n00:00:00,000 --> 00:00:01,000\nHello"))
	}))
	defer server.Close()
	c := NewClient(Config{Server: server.URL}, Session{Token: "secret"})
	if _, err := c.Subtitle(context.Background(), "item", "source", 12); err != nil {
		t.Fatal(err)
	}
}
