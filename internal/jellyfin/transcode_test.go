package jellyfin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"misterfin-crt/internal/media"
)

func TestTranscodeConfig(t *testing.T) {
	for _, tc := range []struct {
		name, lines string
		want        TranscodeProfile
	}{
		{"default", "", DefaultTranscodeProfile()},
		{"dimensions", "640x480\n", TranscodeProfile{640, 480, 12000000}},
		{"complete", "640x480@8000000\n", TranscodeProfile{640, 480, 8000000}},
		{"minimum", "160x120@100000\n", TranscodeProfile{160, 120, 100000}},
		{"maximum", "1920x1080@50000000\n", TranscodeProfile{1920, 1080, 50000000}},
		{"last dimensions retain bitrate", "480x270@4000000\n640x480\n", TranscodeProfile{640, 480, 4000000}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "jellyfin.conf")
			// Options can precede the URL without displacing positional credentials.
			if err := os.WriteFile(path, []byte(tc.lines+"http://example.test\nNTSC\nkey\nname\nDEBUGLOG\n"), 0600); err != nil {
				t.Fatal(err)
			}
			c, err := LoadConfig(path)
			if err != nil {
				t.Fatal(err)
			}
			if c.Transcode != tc.want || c.APIKey != "key" || c.Username != "name" || c.TVMode != "NTSC" || !c.DebugLog {
				t.Fatalf("wrong profile or displaced credentials: %+v", c)
			}
		})
	}
}

func TestInvalidTranscodeProfileCannotBecomeCredentials(t *testing.T) {
	for _, line := range []string{
		"0x480", "159x480", "1921x480", "640x119", "640x1081", "640x480@99999", "640x480@50000001",
		"640x480@0", "640x480@", "640x480@private-value", "640x480@8000000@1", "640x480junk", "640x", "640X480", "640 x480", "-640x480", "+640x480", "640x-480", "640x480@-1", "999999999999999999999999999x480", "640x480@999999999999999999999999999",
	} {
		path := filepath.Join(t.TempDir(), "jellyfin.conf")
		if err := os.WriteFile(path, []byte("http://example.test\n"+line+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
		c, err := LoadConfig(path)
		if err == nil || !strings.Contains(err.Error(), "transcode profile on line 2") {
			t.Errorf("%q: missing profile error: %v", line, err)
			continue
		}
		if strings.Contains(err.Error(), line) || c.APIKey != "" {
			t.Errorf("invalid line leaked or became a credential")
		}
	}
}

func TestCustomProfilePreservesVideoChoicesAndFrameRate(t *testing.T) {
	for _, ntsc := range []bool{false, true} {
		c := NewClient(Config{Server: "http://example.test", Transcode: TranscodeProfile{640, 480, 8000000}}, Session{Token: "private"})
		fps := "25"
		if ntsc {
			fps = "30"
		}
		for _, ticks := range []int64{0, 900000000} {
			raw := prepareTestVideo(t, c, media.VideoRequest{Item: Item{ID: "item"}, SessionID: "session", StartTicks: ticks, NTSC: ntsc, SourceID: "source", Tracks: media.TrackSelection{AudioIndex: 7, SubtitleIndex: 12}, BurnSubtitle: 12}).URL
			u, err := url.Parse(raw)
			if err != nil {
				t.Fatal(err)
			}
			q := u.Query()
			for key, want := range map[string]string{"maxWidth": "640", "maxHeight": "480", "videoBitRate": "8000000", "maxFramerate": fps, "startTimeTicks": strconv.FormatInt(ticks, 10), "audioStreamIndex": "7", "subtitleStreamIndex": "12", "subtitleMethod": "Encode", "allowVideoStreamCopy": "false", "videoCodec": "mpeg2video", "mediaSourceId": "source"} {
				if q.Get(key) != want {
					t.Errorf("%s: %q, want %q", key, q.Get(key), want)
				}
			}
		}
		audio, _ := url.Parse(prepareTestAudio(t, c).URL)
		if audio.Query().Has("maxWidth") || audio.Query().Has("videoBitRate") || audio.Query().Get("static") != "true" {
			t.Fatal("video limits affected music")
		}
	}
	c := NewClient(Config{Server: "http://example.test", Transcode: TranscodeProfile{VideoBitrate: 8000000}}, Session{})
	u, _ := url.Parse(prepareTestVideo(t, c, media.VideoRequest{Item: Item{ID: "item"}, SessionID: "session", NTSC: true, BurnSubtitle: -1}).URL)
	if u.Query().Get("maxWidth") != "720" || u.Query().Get("maxHeight") != "576" {
		t.Fatal("omitted fields lost defaults")
	}
}

func TestCustomLiveProfileReachesNegotiationAndStream(t *testing.T) {
	rate := 30000.0 / 1001
	fps := strconv.FormatFloat(rate, 'f', -1, 64)
	var streamSeen atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/stream" {
			for key, want := range map[string]string{"MaxWidth": "640", "MaxHeight": "480", "VideoBitrate": "8000000", "MaxFramerate": fps, "LiveStreamId": "tuner"} {
				if r.URL.Query().Get(key) != want {
					t.Errorf("stream lost %s", key)
				}
			}
			streamSeen.Store(true)
			fmt.Fprint(w, "media")
			return
		}
		if r.URL.Path != "/Items/channel/PlaybackInfo" {
			t.Errorf("unexpected request %s", r.URL.Path)
			return
		}
		var body struct {
			MaxStreamingBitrate int
			DeviceProfile       struct {
				MaxStreamingBitrate, MaxStaticBitrate int
				CodecProfiles                         []struct {
					Conditions []struct{ Property, Value string }
				}
			}
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		p := body.DeviceProfile
		if body.MaxStreamingBitrate != 8000000 || p.MaxStreamingBitrate != 8000000 || p.MaxStaticBitrate != 8000000 {
			t.Error("wrong streaming budgets")
		}
		if len(p.CodecProfiles) != 1 {
			t.Error("missing video limits")
			return
		}
		limits := map[string]string{}
		for _, condition := range p.CodecProfiles[0].Conditions {
			limits[condition.Property] = condition.Value
		}
		if limits["Width"] != "640" || limits["Height"] != "480" || limits["VideoFramerate"] != fps {
			t.Errorf("wrong limits: %v", limits)
		}
		fmt.Fprintf(w, `{"PlaySessionId":"session","MediaSources":[{"Id":"source","LiveStreamId":"tuner","TranscodingUrl":"/stream?MaxWidth=640&MaxHeight=480&VideoBitrate=8000000&MaxFramerate=%s&LiveStreamId=tuner"}]}`, fps)
	}))
	defer server.Close()
	c := NewClient(Config{Server: server.URL, Transcode: TranscodeProfile{640, 480, 8000000}}, Session{Token: "private"})
	live, err := c.openLive(context.Background(), "channel", rate)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := c.OpenStream(context.Background(), live.StreamURL)
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.ReadAll(stream)
	stream.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !streamSeen.Load() {
		t.Fatal("stream was not opened")
	}
}
