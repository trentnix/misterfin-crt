package playback

import (
	"context"
	"encoding/json"
	"fmt"
	"misterfin-go/internal/jellyfin"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestProgressParser(t *testing.T) {
	p := &positionWriter{positions: make(chan float64, 10)}
	p.Write([]byte("secret URL never returned\nANS_TIME_POS"))
	p.Write([]byte("ITION=1.25\n  2.50 M-V: 0.001\rnan A-V: 0\r -1 A-V: 0\r"))
	for _, want := range []float64{1.25, 2.5} {
		select {
		case got := <-p.positions:
			if got != want {
				t.Fatal(got)
			}
		default:
			t.Fatal("missing position")
		}
	}
	if len(p.positions) != 0 {
		t.Fatal("invalid position accepted")
	}
}
func TestMPlayerCRTAspect(t *testing.T) {
	var item jellyfin.Item
	json.Unmarshal([]byte(`{"MediaStreams":[{"Type":"Video","Width":720,"Height":576,"AspectRatio":"16:9"}]}`), &item)
	for _, h := range []int{240, 288} {
		args := Options{Width: 640, Height: h, Device: "/dev/fb0"}.args(item)
		want := fmt.Sprintf("scale=640:%d,expand=640:%d,dsize=640:%d", h*3/4, h, h)
		if !strings.Contains(strings.Join(args, " "), want) {
			t.Fatalf("args %v", args)
		}
	}
}

func TestPlayerLifecycle(t *testing.T) {
	for _, mode := range []string{"eof", "resume", "cancel", "failure"} {
		t.Run(mode, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "player")
			script := "#!/bin/sh\nprintf 'ANS_TIME_POSITION=0\\n'\ncat /dev/fd/3 >/dev/null\nprintf 'ANS_TIME_POSITION=3\\n'\n"
			if mode == "failure" {
				script = "#!/bin/sh\nexit 1\n"
			}
			if err := os.WriteFile(path, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			runLifecycle(t, path, false, mode, []byte("video"))
		})
	}
}

func TestFFplayDecodesStream(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("FFmpeg unavailable")
	}
	ffplay, err := exec.LookPath("ffplay")
	if err != nil {
		t.Skip("FFplay unavailable")
	}
	t.Setenv("SDL_VIDEODRIVER", "dummy")
	t.Setenv("SDL_AUDIODRIVER", "dummy")
	t.Setenv("SDL_RENDER_DRIVER", "software")
	clip, err := exec.Command(ffmpeg, "-v", "error", "-f", "lavfi", "-i", "color=c=blue:s=160x120:r=25", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000", "-t", "3", "-c:v", "mpeg2video", "-c:a", "mp3", "-f", "mpegts", "pipe:1").Output()
	if err != nil {
		t.Fatal("cannot generate video fixture:", err)
	}
	runLifecycle(t, ffplay, true, "decode", clip)
}

func runLifecycle(t *testing.T, player string, headless bool, mode string, clip []byte) {
	t.Helper()
	var mu sync.Mutex
	var events []string
	var states []jellyfin.PlayState
	var saved struct {
		PlaybackPositionTicks int64
		Played                bool
	}
	var session string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/Items/movie":
			if mode == "resume" {
				fmt.Fprint(w, `{"Id":"movie","Type":"Movie","RunTimeTicks":1000000000,"UserData":{"PlaybackPositionTicks":20000000}}`)
			} else {
				fmt.Fprint(w, `{"Id":"movie","Type":"Movie","RunTimeTicks":30000000}`)
			}
		case "/Videos/movie/stream":
			if mode == "resume" && r.URL.Query().Get("startTimeTicks") != "20000000" {
				t.Error("stream did not resume")
			}
			if r.URL.Query().Get("ApiKey") != "private-token" || r.URL.Query().Get("videoCodec") != "mpeg2video" {
				t.Error("invalid stream query")
			}
			mu.Lock()
			session = r.URL.Query().Get("playSessionId")
			mu.Unlock()
			if mode == "cancel" {
				w.WriteHeader(200)
				w.(http.Flusher).Flush()
				<-r.Context().Done()
			} else {
				w.Write(clip)
			}
		default:
			mu.Lock()
			defer mu.Unlock()
			if r.Method != "POST" {
				t.Error("unexpected method")
			}
			events = append(events, r.URL.Path)
			if strings.HasPrefix(r.URL.Path, "/Sessions/") {
				var state jellyfin.PlayState
				json.NewDecoder(r.Body).Decode(&state)
				states = append(states, state)
			} else {
				json.NewDecoder(r.Body).Decode(&saved)
			}
		}
	}))
	defer server.Close()
	client := jellyfin.NewClient(jellyfin.Config{Server: server.URL}, jellyfin.Session{Token: "private-token", UserID: "user", DeviceID: "device"})
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	sawPosition := false
	err := Run(ctx, client, jellyfin.Item{ID: "movie", Type: "Movie"}, Options{Player: player, Headless: headless, Device: "/dev/fb0", Width: 640, Height: 240}, func(int64) {
		sawPosition = true
		if mode == "cancel" {
			cancel()
		}
	})
	if mode == "failure" {
		if err == nil {
			t.Fatal("missing player failure")
		}
	} else if err != nil {
		t.Fatal(err)
	}
	if mode != "cancel" && ctx.Err() != nil {
		t.Fatal("playback timed out")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(events) == 0 || !strings.Contains(strings.Join(events, ","), "/Sessions/Playing/Stopped") {
		t.Fatal("missing stop report", events)
	}
	for _, state := range states {
		if state.PlaySessionID != session || state.ItemID != "movie" {
			t.Fatal("session identity changed")
		}
	}
	if mode == "failure" {
		if sawPosition || len(events) != 1 {
			t.Fatal("failed player reported playback", events)
		}
		return
	}
	if !sawPosition {
		t.Fatal("player never decoded media")
	}
	if mode == "eof" && (!saved.Played || saved.PlaybackPositionTicks != 0) {
		t.Fatalf("EOF not persisted: %+v", saved)
	}
	if mode == "resume" && (saved.Played || saved.PlaybackPositionTicks != 50000000) {
		t.Fatalf("resume offset not persisted: %+v", saved)
	}
	if mode == "cancel" && saved.Played {
		t.Fatal("cancel marked watched")
	}
}

func TestCancelBeforeStreamHeadersStillStopsSession(t *testing.T) {
	path := filepath.Join(t.TempDir(), "player")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	requested := make(chan struct{})
	stopped := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/Items/movie":
			fmt.Fprint(w, `{"Id":"movie","Type":"Movie"}`)
		case "/Videos/movie/stream":
			close(requested)
			<-r.Context().Done()
		case "/Sessions/Playing/Stopped":
			stopped <- struct{}{}
		default:
			t.Errorf("unexpected report during canceled startup: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	client := jellyfin.NewClient(jellyfin.Config{Server: server.URL}, jellyfin.Session{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		result <- Run(ctx, client, jellyfin.Item{ID: "movie", Type: "Movie"}, Options{Player: path, Headless: true}, func(int64) {})
	}()
	select {
	case <-requested:
	case <-time.After(3 * time.Second):
		t.Fatal("stream not requested")
	}
	cancel()
	select {
	case err := <-result:
		if err != nil {
			t.Fatal("cancel surfaced as a playback failure:", err)
		}
	case <-time.After(7 * time.Second):
		t.Fatal("cancel did not finish")
	}
	select {
	case <-stopped:
	default:
		t.Fatal("transcode session was not stopped")
	}
}

func TestLivePlayerLifecycle(t *testing.T) {
	for _, mode := range []string{"eof", "cancel", "player-failure", "stream-failure"} {
		t.Run(mode, func(t *testing.T) {
			player := filepath.Join(t.TempDir(), "player")
			script := "#!/bin/sh\ncat /dev/fd/3 >/dev/null\nprintf 'ANS_TIME_POSITION=3\\n'\n"
			if mode == "cancel" {
				script = "#!/bin/sh\nprintf 'ANS_TIME_POSITION=3\\n'\nsleep 30\n"
			}
			if mode == "player-failure" {
				script = "#!/bin/sh\nexit 1\n"
			}
			if err := os.WriteFile(player, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			var mu sync.Mutex
			var events []string
			var states []jellyfin.PlayState
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/Items/channel":
					fmt.Fprint(w, `{"Id":"channel","Type":"LiveTvChannel","RunTimeTicks":10000000,"UserData":{"PlaybackPositionTicks":90000000}}`)
				case "/Items/channel/PlaybackInfo":
					fmt.Fprint(w, `{"PlaySessionId":"server-session","MediaSources":[{"Id":"source","LiveStreamId":"tuner","TranscodingUrl":"/negotiated/stream?LiveStreamId=tuner&level=8&VideoCodec=mpeg2video"}]}`)
				case "/negotiated/stream":
					if r.URL.Query().Get("level") != "" || r.URL.Query().Get("LiveStreamId") != "tuner" || r.URL.Query().Get("ApiKey") != "private" {
						t.Error("wrong negotiated stream")
					}
					if mode == "stream-failure" {
						w.WriteHeader(502)
						return
					}
					fmt.Fprint(w, "video")
				default:
					mu.Lock()
					defer mu.Unlock()
					events = append(events, r.URL.Path)
					if r.URL.Path == "/LiveStreams/Close" {
						if r.URL.Query().Get("LiveStreamId") != "tuner" {
							t.Error("wrong tuner closed")
						}
					} else if strings.HasPrefix(r.URL.Path, "/Sessions/") {
						var state jellyfin.PlayState
						json.NewDecoder(r.Body).Decode(&state)
						states = append(states, state)
					} else {
						t.Error("unexpected request (channels must not persist resume state):", r.URL.Path)
					}
				}
			}))
			defer server.Close()
			c := jellyfin.NewClient(jellyfin.Config{Server: server.URL}, jellyfin.Session{Token: "private"})
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			err := Run(ctx, c, jellyfin.Item{ID: "channel", Type: "TvChannel"}, Options{Player: player, Headless: true}, func(ticks int64) {
				if ticks != 30000000 {
					t.Errorf("channel position clamped or resumed: %d", ticks)
				}
				if mode == "cancel" {
					cancel()
				}
			})
			wantFailure := strings.HasSuffix(mode, "failure")
			if (err != nil) != wantFailure {
				t.Fatalf("unexpected result: %v", err)
			}
			mu.Lock()
			defer mu.Unlock()
			if len(events) < 2 || events[len(events)-2] != "/Sessions/Playing/Stopped" || events[len(events)-1] != "/LiveStreams/Close" {
				t.Fatal("stream not stopped and closed", events)
			}
			for _, state := range states {
				if state.LiveStreamID != "tuner" || state.MediaSourceID != "source" || state.PlaySessionID != "server-session" || state.CanSeek == nil || *state.CanSeek {
					t.Fatalf("wrong live state: %+v", state)
				}
			}
			stopped := states[len(states)-1]
			if stopped.Failed == nil || *stopped.Failed != wantFailure {
				t.Fatal("incorrect stopped failure state")
			}
		})
	}
}
