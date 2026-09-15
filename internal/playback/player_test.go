package playback

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"misterfin-crt/internal/jellyfin"
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
			if mode == "audio-decode" {
				fmt.Fprint(w, `{"Id":"movie","Type":"Audio","RunTimeTicks":30000000,"UserData":{"PlaybackPositionTicks":20000000}}`)
			} else if mode == "resume" {
				fmt.Fprint(w, `{"Id":"movie","Type":"Movie","RunTimeTicks":1000000000,"UserData":{"PlaybackPositionTicks":20000000}}`)
			} else {
				fmt.Fprint(w, `{"Id":"movie","Type":"Movie","RunTimeTicks":30000000}`)
			}
		case "/Videos/movie/stream", "/Audio/movie/stream":
			if mode == "resume" && r.URL.Query().Get("startTimeTicks") != "20000000" {
				t.Error("stream did not resume")
			}
			if mode == "audio-decode" {
				if r.URL.Path != "/Audio/movie/stream" || r.URL.Query().Get("static") != "true" || len(r.URL.Query()) != 3 {
					t.Error("audio did not use original stream")
				}
			} else if r.URL.Query().Get("ApiKey") != "private-token" || r.URL.Query().Get("videoCodec") != "mpeg2video" {
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
	decoderKind := DecoderMPlayer
	if headless {
		decoderKind = DecoderFFplay
	}
	sawPosition := false
	err := Run(ctx, client, Config{VideoDecoder: DecoderConfig{Kind: decoderKind, Player: player}, AudioDecoder: DecoderConfig{Kind: decoderKind, Player: player}, Device: "/dev/fb0", Width: 640, Height: 240}, Request{Item: jellyfin.Item{ID: "movie", Type: "Movie"}, Callbacks: Callbacks{Position: func(int64) {
		sawPosition = true
		if mode == "cancel" {
			cancel()
		}
	}}})
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
		if mode == "audio-decode" && state.PlayMethod != "DirectStream" {
			t.Error("audio reported transcoding")
		}
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
		result <- Run(ctx, client, Config{VideoDecoder: DecoderConfig{Kind: DecoderFFplay, Player: path}, AudioDecoder: DecoderConfig{Kind: DecoderFFplay, Player: path}}, Request{Item: jellyfin.Item{ID: "movie", Type: "Movie"}, Callbacks: Callbacks{Position: func(int64) {}}})
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
			err := Run(ctx, c, Config{VideoDecoder: DecoderConfig{Kind: DecoderFFplay, Player: player}, AudioDecoder: DecoderConfig{Kind: DecoderFFplay, Player: player}}, Request{Item: jellyfin.Item{ID: "channel", Type: "TvChannel"}, Callbacks: Callbacks{Position: func(ticks int64) {
				if ticks != 30000000 {
					t.Errorf("channel position clamped or resumed: %d", ticks)
				}
				if mode == "cancel" {
					cancel()
				}
			}}})
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

func TestFFplayDecodesOriginalAudio(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("FFmpeg unavailable")
	}
	ffplay, err := exec.LookPath("ffplay")
	if err != nil {
		t.Skip("FFplay unavailable")
	}
	t.Setenv("SDL_AUDIODRIVER", "dummy")
	clip, err := exec.Command(ffmpeg, "-v", "error", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=44100", "-t", "3", "-c:a", "flac", "-f", "flac", "pipe:1").Output()
	if err != nil {
		t.Fatal(err)
	}
	runLifecycle(t, ffplay, true, "audio-decode", clip)
}

func TestControllableAudioReportsPauseAndResume(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("FFmpeg unavailable")
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("Python unavailable")
	}
	if exec.Command(python, "-c", "import ctypes.util,sys;sys.exit(not ctypes.util.find_library('mpv'))").Run() != nil {
		t.Skip("libmpv unavailable")
	}
	clip, err := exec.Command(ffmpeg, "-v", "error", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=44100", "-t", "40", "-c:a", "pcm_s16le", "-f", "wav", "pipe:1").Output()
	if err != nil {
		t.Fatal(err)
	}
	helper, err := filepath.Abs("../../tools/ghostty/video_player.py")
	if err != nil {
		t.Fatal(err)
	}
	wrapper := filepath.Join(t.TempDir(), "audio.py")
	if err = os.WriteFile(wrapper, []byte(fmt.Sprintf("import runpy,sys\nsys.argv += ['--audio','null']\nrunpy.run_path(%q,run_name='__main__')\n", helper)), 0600); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var states []jellyfin.PlayState
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/Items/track":
			fmt.Fprint(w, `{"Id":"track","Type":"Audio","RunTimeTicks":400000000}`)
		case "/Audio/track/stream":
			http.ServeContent(w, r, "track.wav", time.Time{}, bytes.NewReader(clip))
		default:
			if strings.HasPrefix(r.URL.Path, "/Sessions/") {
				var state jellyfin.PlayState
				json.NewDecoder(r.Body).Decode(&state)
				mu.Lock()
				states = append(states, state)
				mu.Unlock()
			}
		}
	}))
	defer server.Close()
	c := jellyfin.NewClient(jellyfin.Config{Server: server.URL}, jellyfin.Session{})
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	controls := make(chan Control, 4)
	first, paused, resumed, advanced := true, false, false, false
	seekStage := 0
	err = Run(ctx, c, Config{VideoDecoder: DecoderConfig{Kind: DecoderFFplay}, AudioDecoder: DecoderConfig{Kind: DecoderPython, Helper: wrapper}}, Request{Item: jellyfin.Item{ID: "track", Type: "Audio"}, Controls: controls, Callbacks: Callbacks{Paused: func(value bool) {
		if value {
			paused = true
			seekStage = 1
			controls <- Control{Kind: "seek", Seconds: 10}
		} else {
			resumed = true
		}
	}, Position: func(ticks int64) {
		if first {
			first = false
			controls <- Control{Kind: "pause"}
		}
		if paused && !resumed {
			if seekStage == 1 && ticks >= 10*10000000 {
				seekStage = 2
				controls <- Control{Kind: "seek", Seconds: -10}
			} else if seekStage == 2 && ticks < 2*10000000 {
				seekStage = 3
				controls <- Control{Kind: "pause"}
			}
		}
		if resumed && ticks >= 10000000 {
			advanced = true
			cancel()
		}
	}}})
	if err != nil || !paused || !resumed || !advanced || seekStage != 3 {
		t.Fatalf("audio controls failed: pause=%v resume=%v advanced=%v error=%v", paused, resumed, advanced, err)
	}
	mu.Lock()
	defer mu.Unlock()
	reportedPause := false
	for _, state := range states {
		if state.PlayMethod != "DirectStream" {
			t.Fatal("audio did not report direct streaming")
		}
		reportedPause = reportedPause || state.IsPaused
	}
	if !reportedPause {
		t.Fatal("pause state was not reported")
	}
}

func TestBufferingProtocol(t *testing.T) {
	p := &positionWriter{positions: make(chan float64, 10), buffering: make(chan bool, 10)}
	p.Write([]byte("ANS_BUFFER"))
	p.Write([]byte("ING=true\nANS_BUFFERING=invalid\nANS_TIME_POSITION=2\nANS_BUFFERING=false\n"))
	if len(p.buffering) != 2 || !<-p.buffering || <-p.buffering {
		t.Fatal("invalid buffering transitions")
	}
	if len(p.positions) != 1 || <-p.positions != 2 {
		t.Fatal("position feedback lost")
	}
}

func TestVideoStartedProtocol(t *testing.T) {
	p := &positionWriter{positions: make(chan float64, 1), videoStarted: make(chan struct{}, 1)}
	p.Write([]byte("ANS_VIDEO_STARTED=false\nANS_VIDEO_STA"))
	p.Write([]byte("RTED=true\nANS_VIDEO_STARTED=true\n"))
	if len(p.videoStarted) != 1 || len(p.positions) != 0 {
		t.Fatal("first-frame feedback was lost or invented a playback position")
	}
}

func TestVideoStartedDoesNotWaitForPosition(t *testing.T) {
	path := filepath.Join(t.TempDir(), "player")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf 'ANS_VIDEO_STARTED=true\\n'\nsleep 30\n"), 0700); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/Items/movie" {
			fmt.Fprint(w, `{"Id":"movie","Type":"Movie"}`)
		} else if r.URL.Path == "/Videos/movie/stream" {
			fmt.Fprint(w, "media")
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{}, 1)
	positions := make(chan int64, 1)
	done := make(chan error, 1)
	client := jellyfin.NewClient(jellyfin.Config{Server: server.URL}, jellyfin.Session{})
	go func() {
		done <- Run(ctx, client, Config{VideoDecoder: DecoderConfig{Kind: DecoderMPlayer, Player: path}, AudioDecoder: DecoderConfig{Kind: DecoderMPlayer, Player: path}, Width: 640, Height: 240}, Request{Item: jellyfin.Item{ID: "movie", Type: "Movie"}, Callbacks: Callbacks{VideoStarted: func() { started <- struct{}{} }, Position: func(ticks int64) { positions <- ticks }}})
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first-frame callback waited for a position report")
	}
	if len(positions) != 0 {
		t.Fatal("first-frame feedback invented a position")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("decoder did not stop")
	}
}

func TestExplicitVideoStartOverridesServerResume(t *testing.T) {
	player := filepath.Join(t.TempDir(), "player")
	if err := os.WriteFile(player, []byte("#!/bin/sh\nprintf 'ANS_TIME_POSITION=0\n'\nsleep 30\n"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ target, want int64 }{{-1, 0}, {0, 0}, {600000000, 600000000}, {2000000000, 990000000}} {
		t.Run(fmt.Sprint(tc.target), func(t *testing.T) {
			requests := make(chan string, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/Items/movie":
					fmt.Fprint(w, `{"Id":"movie","Type":"Movie","RunTimeTicks":1000000000,"UserData":{"Played":true,"PlaybackPositionTicks":200000000}}`)
				case "/Videos/movie/stream":
					requests <- r.URL.Query().Get("startTimeTicks")
					fmt.Fprint(w, "media")
				default:
					w.WriteHeader(http.StatusNoContent)
				}
			}))
			defer server.Close()
			c := jellyfin.NewClient(jellyfin.Config{Server: server.URL}, jellyfin.Session{Token: "test", UserID: "user", DeviceID: "device"})
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			var got int64
			err := Run(ctx, c, Config{VideoDecoder: DecoderConfig{Kind: DecoderFFplay, Player: player}, AudioDecoder: DecoderConfig{Kind: DecoderFFplay, Player: player}}, Request{Item: jellyfin.Item{ID: "movie", Type: "Movie"}, StartTicks: &tc.target, Callbacks: Callbacks{Position: func(ticks int64) { got = ticks; cancel() }}})
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("position=%d want=%d", got, tc.want)
			}
			select {
			case start := <-requests:
				if start != fmt.Sprint(tc.want) {
					t.Fatal("stream offset", start)
				}
			default:
				t.Fatal("no stream request")
			}
		})
	}
}

func TestPreparedPlaybackWaitsAtStartGate(t *testing.T) {
	player := filepath.Join(t.TempDir(), "player")
	started := filepath.Join(t.TempDir(), "started")
	if err := os.WriteFile(player, []byte("#!/bin/sh\ntouch \"$PLAYER_STARTED\"\nprintf 'ANS_TIME_POSITION=0\\n'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PLAYER_STARTED", started)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/Items/movie":
			fmt.Fprint(w, `{"Id":"movie","Type":"Movie"}`)
		case "/Videos/movie/stream":
			fmt.Fprint(w, "media")
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()
	client := jellyfin.NewClient(jellyfin.Config{Server: server.URL}, jellyfin.Session{})
	gate := make(chan struct{})
	ready := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- Run(context.Background(), client, Config{VideoDecoder: DecoderConfig{Kind: DecoderFFplay, Player: player}, AudioDecoder: DecoderConfig{Kind: DecoderFFplay, Player: player}}, Request{Item: jellyfin.Item{ID: "movie", Type: "Movie"}, Start: gate, Callbacks: Callbacks{Ready: func() { close(ready) }, Position: func(int64) {}}})
	}()
	select {
	case <-ready:
	case <-time.After(3 * time.Second):
		t.Fatal("replacement stream was not prepared")
	}
	if _, err := os.Stat(started); !os.IsNotExist(err) {
		t.Fatal("decoder started before the handoff gate opened")
	}
	close(gate)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("prepared playback did not start")
	}
	if _, err := os.Stat(started); err != nil {
		t.Fatal("decoder did not start after handoff:", err)
	}
}

func TestAsyncCleanupDoesNotDelayPlaybackReturn(t *testing.T) {
	player := filepath.Join(t.TempDir(), "player")
	if err := os.WriteFile(player, []byte("#!/bin/sh\nprintf 'ANS_TIME_POSITION=0\\n'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	cleanupStarted := make(chan struct{})
	releaseCleanup := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/Items/movie":
			fmt.Fprint(w, `{"Id":"movie","Type":"Movie"}`)
		case "/Videos/movie/stream":
			fmt.Fprint(w, "media")
		case "/Sessions/Playing/Stopped":
			close(cleanupStarted)
			<-releaseCleanup
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()
	client := jellyfin.NewClient(jellyfin.Config{Server: server.URL}, jellyfin.Session{})
	fast := make(chan struct{})
	close(fast)
	returned := make(chan error, 1)
	go func() {
		returned <- Run(context.Background(), client, Config{VideoDecoder: DecoderConfig{Kind: DecoderFFplay, Player: player}, AudioDecoder: DecoderConfig{Kind: DecoderFFplay, Player: player}}, Request{Item: jellyfin.Item{ID: "movie", Type: "Movie"}, AsyncCleanup: fast, Callbacks: Callbacks{Position: func(int64) {}}})
	}()
	select {
	case err := <-returned:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(500 * time.Millisecond):
		close(releaseCleanup)
		t.Fatal("session cleanup delayed the playback handoff")
	}
	select {
	case <-cleanupStarted:
	case <-time.After(3 * time.Second):
		close(releaseCleanup)
		t.Fatal("asynchronous cleanup did not start")
	}
	close(releaseCleanup)
}

// Hardware video must retain the C player's audio synchronization policy.

// The negotiated cap must follow physical output geometry without changing
// progressive NTSC or PAL. Exercise preparation through the actual HTTP request.
func TestLiveFrameRateMatchesOutput(t *testing.T) {
	for _, tc := range []struct {
		height int
		want   string
	}{{240, "30"}, {288, "25"}, {480, "29.97002997002997"}, {576, "25"}} {
		t.Run(fmt.Sprint(tc.height), func(t *testing.T) {
			rates := make(chan string, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/Items/channel":
					fmt.Fprint(w, `{"Id":"channel","Type":"LiveTvChannel"}`)
				case "/Items/channel/PlaybackInfo":
					rate := ""
					var request struct {
						DeviceProfile struct {
							CodecProfiles []struct {
								Conditions []struct{ Property, Value string }
							}
						}
					}
					if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
						t.Error(err)
					}
					for _, profile := range request.DeviceProfile.CodecProfiles {
						for _, condition := range profile.Conditions {
							if condition.Property == "VideoFramerate" {
								rate = condition.Value
							}
						}
					}
					rates <- rate
					fmt.Fprint(w, `{"PlaySessionId":"play","MediaSources":[{"Id":"source","LiveStreamId":"tuner","TranscodingUrl":"/stream"}]}`)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			c := jellyfin.NewClient(jellyfin.Config{Server: server.URL}, jellyfin.Session{UserID: "user"})
			session, err := preparePlayback(context.Background(), c, Config{Height: tc.height}, Request{Item: jellyfin.Item{ID: "channel", Type: "TvChannel"}}, trackPreparation{})
			if err != nil || session == nil {
				t.Fatalf("prepare: %v", err)
			}
			if rate := <-rates; rate != tc.want {
				t.Fatalf("frame-rate cap %q, want %q", rate, tc.want)
			}
		})
	}
}
