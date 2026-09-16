package jellyfin

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"
	"time"
)

func TestLiveProfileAndNegotiatedURL(t *testing.T) {
	for _, tc := range []struct {
		name  string
		rate  float64
		value string
	}{
		{"PAL", 25, "25"},
		{"NTSC progressive", 30, "30"},
		{"NTSC interlaced", 30000.0 / 1001, "29.97002997002997"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" || r.URL.Path != "/Items/channel/PlaybackInfo" {
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
				}
				var got map[string]any
				json.NewDecoder(r.Body).Decode(&got)
				fps := tc.value
				wantJSON := fmt.Sprintf(`{"UserId":"user","StartTimeTicks":0,"IsPlayback":true,"AutoOpenLiveStream":true,"EnableDirectPlay":false,"EnableDirectStream":false,"EnableTranscoding":true,"AllowVideoStreamCopy":false,"AllowAudioStreamCopy":false,"MaxStreamingBitrate":12000000,"DeviceProfile":{"Name":"MiSTerFin","MaxStreamingBitrate":12000000,"MaxStaticBitrate":12000000,"DirectPlayProfiles":[],"TranscodingProfiles":[{"Container":"ts","Type":"Video","Protocol":"http","AudioCodec":"mp3","VideoCodec":"mpeg2video","Context":"Streaming","MaxAudioChannels":"2"}],"CodecProfiles":[{"Type":"Video","Codec":"mpeg2video","Conditions":[{"Condition":"LessThanEqual","Property":"Width","Value":"720","IsRequired":true},{"Condition":"LessThanEqual","Property":"Height","Value":"576","IsRequired":true},{"Condition":"LessThanEqual","Property":"VideoFramerate","Value":"%s","IsRequired":true}]}],"SubtitleProfiles":[]}}`, fps)
				var want map[string]any
				json.Unmarshal([]byte(wantJSON), &want)
				if !reflect.DeepEqual(got, want) {
					t.Errorf("unexpected codec or frame-rate limit: %+v", got)
				}
				fmt.Fprint(w, `{"PlaySessionId":"session","MediaSources":[{"Id":"source","LiveStreamId":"tuner","MediaStreams":[{"Type":"Video","Width":720,"Height":576,"AspectRatio":"16:9"}],"TranscodingUrl":"/Videos/channel/stream.ts?Level=8&mpeg2video-level=2&keep=value&LiveStreamId=tuner"}]}`)
			}))
			defer server.Close()
			c := NewClient(Config{Server: server.URL}, Session{UserID: "user", Token: "private-token"})
			live, err := c.openLive(context.Background(), "channel", tc.rate)
			if err != nil {
				t.Fatal(err)
			}
			if live.LiveStreamID != "tuner" || live.MediaSourceID != "source" || live.PlaySessionID != "session" {
				t.Fatal("missing stream identity")
			}
			if len(live.MediaStreams) != 1 || live.MediaStreams[0].AspectRatio != "16:9" {
				t.Fatal("negotiated video geometry was lost")
			}
			u, _ := url.Parse(live.StreamURL)
			want := url.Values{"keep": {"value"}, "LiveStreamId": {"tuner"}, "ApiKey": {"private-token"}}
			if !reflect.DeepEqual(u.Query(), want) {
				t.Fatal("negotiated query changed")
			}
		})
	}
}

func TestLiveURLRejectsExternalSourcesAndPreservesBasePath(t *testing.T) {
	c := NewClient(Config{Server: "https://server/jellyfin"}, Session{Token: "private"})
	for _, raw := range []string{"", "https://other/stream", "//other/stream", "relative/stream", "/stream?bad=%xx"} {
		if _, err := c.liveURL(raw); err == nil {
			t.Errorf("accepted %q", raw)
		}
	}
	got, err := c.liveURL("/Videos/live/stream?apiKey=existing&level=8&tag=a&tag=b")
	if err != nil || got != "https://server/jellyfin/Videos/live/stream?apiKey=existing&tag=a&tag=b" {
		t.Fatal("base path or authentication changed")
	}
}

func TestLiveNegotiationReleasesTunerOnFailureOrCancel(t *testing.T) {
	for _, mode := range []string{"invalid", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			closed := make(chan string, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/LiveStreams/Close" {
					closed <- r.URL.Query().Get("LiveStreamId")
					w.WriteHeader(204)
					return
				}
				raw := "/Videos/live/stream"
				if mode == "invalid" {
					raw = "https://external/stream"
				} else {
					cancel()
					time.Sleep(30 * time.Millisecond)
				}
				fmt.Fprintf(w, `{"PlaySessionId":"s","MediaSources":[{"Id":"m","LiveStreamId":"tuner","TranscodingUrl":%q}]}`, raw)
			}))
			defer server.Close()
			c := NewClient(Config{Server: server.URL}, Session{})
			if _, err := c.openLive(ctx, "channel", 30); err == nil {
				t.Fatal("missing failure")
			}
			select {
			case id := <-closed:
				if id != "tuner" {
					t.Fatal(id)
				}
			default:
				t.Fatal("tuner leaked")
			}
		})
	}
}

func TestLiveRejectsInvalidFrameRateBeforeRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("invalid frame-rate limit reached server")
	}))
	defer server.Close()
	c := NewClient(Config{Server: server.URL}, Session{})
	for _, rate := range []float64{0, -1, math.NaN(), math.Inf(1), math.Inf(-1)} {
		if _, err := c.openLive(context.Background(), "channel", rate); err == nil {
			t.Errorf("accepted invalid frame-rate limit %v", rate)
		}
	}
}
