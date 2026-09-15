package playback

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"misterfin-crt/internal/jellyfin"
	playerapi "misterfin-crt/internal/player"
	desktopplayer "misterfin-crt/internal/player/ffplay"
	"misterfin-crt/internal/player/mplayer"
	inlineplayer "misterfin-crt/internal/player/pythonhelper"
)

func TestOriginalAndZoomGeometry(t *testing.T) {
	// Anamorphic PAL input must use display aspect, not 720/576.
	wide := jellyfin.Item{Type: "Movie", MediaStreams: []jellyfin.MediaStream{{Type: "Video", Width: 720, Height: 576, AspectRatio: "16:9"}}}
	for _, height := range []int{240, 288, 480, 576} {
		for _, mode := range []PictureMode{PictureOriginal, PictureZoom43} {
			d, err := selectDecoder(Config{Height: height, VideoDecoder: mplayer.Decoder{Width: 640, Height: height}, AudioDecoder: mplayer.Decoder{Width: 640, Height: height}}, wide, mode)
			if err != nil {
				t.Fatal(err)
			}
			want := fmt.Sprintf("misterfin=640:%d:1.777777778:%d", height, mode)
			args := d.Args(wide, "")
			if !slices.Contains(args, want) {
				t.Fatalf("height %d, mode %d: %v", height, mode, args)
			}
		}
	}
}

func TestZoomAcrossDecodersAndAspectRatios(t *testing.T) {
	for _, kind := range []string{"Episode", "TvChannel", "LiveTvChannel"} {
		for _, aspect := range []string{"16:9", "235:100", "4:3", "1:1"} {
			item := jellyfin.Item{Type: kind, MediaStreams: []jellyfin.MediaStream{{Type: "Video", Width: 720, Height: 576, AspectRatio: aspect}}}
			for _, options := range []Config{
				{VideoDecoder: desktopplayer.Decoder{}, AudioDecoder: desktopplayer.Decoder{}},
				{VideoDecoder: inlineplayer.Decoder{Script: "video.py", Output: "frame", Width: 640, Height: 240}, AudioDecoder: inlineplayer.Decoder{Script: "video.py", Output: "frame", Width: 640, Height: 240}, Height: 240},
			} {
				for _, mode := range []PictureMode{PictureOriginal, PictureZoom43} {
					d, err := selectDecoder(options, item, mode)
					if err != nil {
						t.Fatal(err)
					}
					args := strings.Join(d.Args(item, ""), " ")
					zoom := strings.Contains(args, "crop=") || strings.Contains(args, "--zoom-4-3")
					if zoom != (mode == PictureZoom43) {
						t.Fatalf("%T, aspect %s, mode %d: %s", d, aspect, mode, args)
					}
				}
			}
		}
	}
	if PictureZoom43.Zooms(jellyfin.Item{Type: "Audio"}) {
		t.Fatal("zoom enabled for audio")
	}
}

func TestInvalidPictureModeRejected(t *testing.T) {
	_, err := videoTracks(jellyfin.Item{}, trackPreparation{explicit: &TrackOptions{Picture: PictureMode(99)}, clientSubtitles: true})
	if err == nil {
		t.Fatal("invalid picture mode accepted")
	}
}

func TestNativePictureProtocolAndAcknowledgments(t *testing.T) {
	var commands bytes.Buffer
	d := mplayer.Decoder{}
	if err := d.SetPicture(playerapi.Control{Stdin: &commands}, PictureZoom43, 42); err != nil {
		t.Fatal(err)
	}
	if commands.String() != "pausing_keep_force misterfin_picture 1 42\n" {
		t.Fatal(commands.String())
	}
	p := &playerProcess{pictures: make(chan PictureResult, 4)}
	w := d.Feedback(p.publishFeedback)
	w.Write([]byte("ANS_PICTURE_"))
	w.Write([]byte("MODE=42,1\nANS_PICTURE_MODE=43,-1\nANS_PICTURE_MODE=44,9\n"))
	first, second := <-p.pictures, <-p.pictures
	if first.Request != 42 || first.Mode != PictureZoom43 || first.Err != nil || second.Request != 43 || second.Err == nil || len(p.pictures) != 0 {
		t.Fatal("invalid picture acknowledgment")
	}
}

func TestInlinePictureUsesLiveControlProtocol(t *testing.T) {
	d, err := selectDecoder(Config{VideoDecoder: inlineplayer.Decoder{Script: "video.py", Output: "frame", Width: 640, Height: 240}, AudioDecoder: inlineplayer.Decoder{Script: "video.py", Output: "frame", Width: 640, Height: 240}, Height: 240}, jellyfin.Item{Type: "Movie"}, PictureOriginal)
	if err != nil {
		t.Fatal(err)
	}
	setter, ok := d.(playerapi.PictureSetter)
	if !ok {
		t.Fatal("inline decoder does not advertise live picture changes")
	}
	var commands bytes.Buffer
	for request, mode := range []PictureMode{PictureZoom43, PictureOriginal} {
		if err := setter.SetPicture(playerapi.Control{Stdin: &commands}, mode, request+1); err != nil {
			t.Fatal(err)
		}
	}
	if commands.String() != "picture 1 1\npicture 0 2\n" {
		t.Fatal("wrong inline picture protocol")
	}
}

func TestNativePictureRequestStaysInCurrentSession(t *testing.T) {
	for _, kind := range []string{"Movie", "TvChannel"} {
		t.Run(kind, func(t *testing.T) {
			var negotiated, closed atomic.Int32
			var streams atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/Items/film/PlaybackInfo" {
					negotiated.Add(1)
					fmt.Fprint(w, `{"PlaySessionId":"live-session","MediaSources":[{"Id":"source","LiveStreamId":"tuner","TranscodingUrl":"/Videos/film/stream","MediaStreams":[{"Type":"Video","AspectRatio":"16:9"},{"Type":"Audio","Index":-1}]}]}`)
					return
				}
				if r.URL.Path == "/LiveStreams/Close" {
					closed.Add(1)
				}
				if r.Method != "GET" {
					w.WriteHeader(http.StatusNoContent)
					return
				}
				if strings.HasPrefix(r.URL.Path, "/Items/") {
					w.Write([]byte(`{"Id":"film","Type":"Movie","RunTimeTicks":1000000000,"MediaStreams":[{"Type":"Video","Width":720,"Height":576,"AspectRatio":"16:9"}]}`))
				} else if strings.HasPrefix(r.URL.Path, "/Videos/") {
					streams.Add(1)
					w.Write([]byte("test video"))
				} else {
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			path := filepath.Join(t.TempDir(), "player")
			script := "#!/bin/sh\nprintf 'ANS_TIME_POSITION=2\\n'\nwhile read keep command mode request; do\ncase \"$command\" in\nmisterfin_picture) printf 'ANS_PICTURE_MODE=%s,%s\\n' \"$request\" \"$mode\";;\nesac\ndone\n"
			if err := os.WriteFile(path, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			controls := make(chan Control, 4)
			info := make(chan VideoTracks, 1)
			positions := make(chan int64, 4)
			pictures := make(chan PictureResult, 4)
			done := make(chan error, 1)
			client := jellyfin.NewClient(jellyfin.Config{Server: server.URL}, jellyfin.Session{})
			preferences := NewPreferences(t.TempDir(), nil)
			defer preferences.Close()
			go func() {
				done <- Run(ctx, client, Config{Preferences: preferences, Height: 240, VideoDecoder: mplayer.Decoder{Player: path, Width: 640, Height: 240}, AudioDecoder: mplayer.Decoder{Player: path, Width: 640, Height: 240}}, Request{Item: jellyfin.Item{ID: "film", Type: kind}, Controls: controls, Callbacks: Callbacks{TrackInfo: func(v VideoTracks) { info <- v }, Picture: func(v PictureResult) { pictures <- v }, Position: func(p int64) { positions <- p }}})
			}()
			select {
			case v := <-info:
				if !v.LivePicture || kind == "TvChannel" && (v.SourceID != "source" || len(v.Streams) != 2 || v.ClientSubtitles) {
					t.Fatal("native live picture capability missing")
				}
			case <-time.After(3 * time.Second):
				t.Fatal("no metadata")
			}
			select {
			case <-positions:
			case <-time.After(3 * time.Second):
				t.Fatal("no position")
			}
			for n, mode := range []PictureMode{PictureZoom43, PictureOriginal, PictureZoom43} {
				controls <- Control{Kind: SetPicture, Picture: mode, Request: n + 1}
				select {
				case r := <-pictures:
					if r.Request != n+1 || r.Mode != mode || r.Err != nil {
						t.Fatal(r)
					}
				case <-time.After(3 * time.Second):
					t.Fatal("no picture acknowledgment")
				}
				if saved := preferences.load(preferenceKey(client, "film")); kind == "Movie" && (saved == nil || saved.Picture != mode) || kind == "TvChannel" && saved != nil {
					t.Fatal("acknowledged native picture choice was not saved")
				}
			}
			if streams.Load() != 1 {
				t.Fatal("picture change reopened media")
			}
			controls <- Control{Kind: SetPicture, Picture: PictureMode(99), Request: 99}
			select {
			case result := <-pictures:
				if result.Err == nil {
					t.Fatal("invalid picture request succeeded")
				}
			case <-time.After(3 * time.Second):
				t.Fatal("no failure acknowledgment")
			}
			if saved := preferences.load(preferenceKey(client, "film")); kind == "Movie" && (saved == nil || saved.Picture != PictureZoom43) || kind == "TvChannel" && saved != nil {
				t.Fatal("failed picture request changed the saved choice")
			}
			cancel()
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("player did not stop")
			}
			if kind == "TvChannel" && (negotiated.Load() != 1 || closed.Load() != 1) {
				t.Fatalf("picture changes affected tuner lifetime: opened %d, closed %d", negotiated.Load(), closed.Load())
			}
		})
	}
}
