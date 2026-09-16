package jellyfin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"

	"misterfin-crt/internal/media"
)

// The adapter must preserve the established stream profile and wire reporting
// while exposing only shared values to playback.
func TestPreparedRecordedStreamsPreserveJellyfinBehavior(t *testing.T) {
	reports := make(chan PlayState, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/Sessions/Playing/Progress" {
			t.Errorf("unexpected request %s", r.URL.Path)
		}
		var reported PlayState
		if err := json.NewDecoder(r.Body).Decode(&reported); err != nil {
			t.Error(err)
		}
		reports <- reported
	}))
	defer server.Close()
	client := NewClient(Config{Server: server.URL}, Session{UserID: "viewer", DeviceID: "device", Token: "private"})
	var service media.Playback = client
	if got := service.Identity(); got != (media.Identity{Server: server.URL, User: "viewer"}) {
		t.Fatalf("changed cache/preference scope: %+v", got)
	}
	for _, ntsc := range []bool{false, true} {
		stream, err := service.PrepareVideo(t.Context(), media.VideoRequest{
			Item: media.Item{ID: "movie"}, SessionID: "session", StartTicks: 125000000,
			NTSC: ntsc, SourceID: "source", Tracks: media.TrackSelection{AudioIndex: 3, SubtitleIndex: 7}, BurnSubtitle: 7,
		})
		if err != nil {
			t.Fatal(err)
		}
		u, err := url.Parse(stream.URL)
		if err != nil {
			t.Fatal("invalid stream URL")
		}
		fps := "25"
		if ntsc {
			fps = "30"
		}
		for key, want := range map[string]string{"videoCodec": "mpeg2video", "container": "ts", "audioCodec": "mp3", "audioChannels": "2", "audioSampleRate": "48000", "maxFramerate": fps, "startTimeTicks": "125000000", "mediaSourceId": "source", "audioStreamIndex": "3", "subtitleStreamIndex": "7", "subtitleMethod": "Encode"} {
			if got := u.Query().Get(key); got != want {
				t.Errorf("%s = %q, want %q", key, got, want)
			}
		}
		if stream.SessionID != "session" || stream.SourceID != "source" || stream.Release != nil {
			t.Fatal("recorded stream changed ownership or identity")
		}
		if err := stream.Reports.ReportPlaying(t.Context(), "progress", media.PlayState{ItemID: "movie", PositionTicks: 125000000}); err != nil {
			t.Fatal(err)
		}
		reported := <-reports
		if reported.PlayMethod != "Transcode" || reported.PositionTicks != 125000000 || reported.LiveStreamID != "" {
			t.Fatalf("changed video report: %+v", reported)
		}
	}
	stream, err := service.PrepareAudio(t.Context(), media.Item{ID: "song"}, "audio-session")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(stream.URL)
	if u.Path != "/Audio/song/stream" || u.Query().Get("static") != "true" || stream.Release != nil {
		t.Fatal("audio no longer uses its original source")
	}
	if err := stream.Reports.ReportPlaying(t.Context(), "progress", media.PlayState{ItemID: "song", Audio: true}); err != nil {
		t.Fatal(err)
	}
	reported := <-reports
	if reported.PlayMethod != "DirectStream" {
		t.Fatalf("changed audio report: %+v", reported)
	}
}

func TestPreparedLiveStreamRetainsPrivateTunerOwnership(t *testing.T) {
	events := make(chan string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/Items/channel/PlaybackInfo":
			fmt.Fprint(w, `{"PlaySessionId":"session","MediaSources":[{"Id":"source","LiveStreamId":"tuner","TranscodingUrl":"/live.ts","MediaStreams":[{"Type":"Video","Width":640,"Height":480}]}]}`)
		case "/Sessions/Playing/Stopped":
			var state PlayState
			if err := json.NewDecoder(r.Body).Decode(&state); err != nil {
				t.Error(err)
			}
			if state.LiveStreamID != "tuner" {
				t.Error("adapter lost the tuner identity")
			}
			events <- "stopped"
		case "/LiveStreams/Close":
			if r.URL.Query().Get("LiveStreamId") != "tuner" {
				t.Error("released the wrong tuner")
			}
			events <- "released"
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
		}
	}))
	defer server.Close()
	var service media.LiveTV = NewClient(Config{Server: server.URL}, Session{})
	ctx, cancel := context.WithCancel(t.Context())
	stream, err := service.PrepareLive(ctx, "channel", 30000.0/1001)
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	if len(stream.Streams) != 1 || stream.SessionID != "session" || stream.SourceID != "source" {
		t.Fatal("negotiated stream metadata was lost")
	}
	if err := stream.Reports.ReportPlaying(t.Context(), "stopped", media.PlayState{ItemID: "channel"}); err != nil {
		t.Fatal(err)
	}
	if stream.Release == nil {
		t.Fatal("live stream has no release operation")
	}
	if err := stream.Release(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := []string{<-events, <-events}; !reflect.DeepEqual(got, []string{"stopped", "released"}) {
		t.Fatalf("unexpected cleanup: %v", got)
	}
}

func TestStreamLimitsAcceptNegotiatedCaseAndRejectInvalidNumbers(t *testing.T) {
	got := streamLimits("http://private-host/stream?MaxWidth=640&MaxHeight=480&VideoBitrate=8000000&MaxFramerate=29.97&ApiKey=private-token")
	want := media.StreamLimits{MaxWidth: 640, MaxHeight: 480, VideoBitrate: 8000000, MaxFrameRate: 29.97}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	for _, raw := range []string{"http://server/audio", "http://server/live?maxWidth=-1&maxHeight=NaN&videoBitRate=Inf&maxFramerate=1000000001", "://invalid"} {
		if got := streamLimits(raw); got != (media.StreamLimits{}) {
			t.Fatalf("invalid or absent limits: %+v", got)
		}
	}
}

func TestRequestStreamReusesConnectionsAndRestrictsHeaders(t *testing.T) {
	peers := make(chan string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		peers <- r.RemoteAddr
		if r.Header.Get("Range") != "bytes=0-4" || r.Header.Get("Accept-Encoding") != "identity" {
			t.Error("byte-range request changed")
		}
		if r.Header.Get("Authorization") != "" {
			t.Error("forwarded an unrelated proxy header")
		}
		fmt.Fprint(w, "audio")
	}))
	defer server.Close()
	client := NewClient(Config{Server: server.URL}, Session{})
	defer client.HTTP.CloseIdleConnections()
	for range 2 {
		response, err := client.RequestStream(t.Context(), "GET", server.URL+"/audio", http.Header{"Range": {"bytes=0-4"}, "Authorization": {"untrusted"}})
		if err != nil {
			t.Fatal(err)
		}
		_, err = io.Copy(io.Discard, response.Body)
		response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	if first, second := <-peers, <-peers; first != second {
		t.Fatal("range requests did not reuse the connection")
	}
	if _, err := client.RequestStream(t.Context(), "GET", "http://other-server/audio", nil); err == nil {
		t.Fatal("accepted a stream outside the configured server")
	}
}
