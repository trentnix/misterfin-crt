package playback

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"mistervision/internal/jellyfin"
	"mistervision/internal/media"
	nativeplayer "mistervision/internal/player/mplayer"
	inlineplayer "mistervision/internal/player/pythonhelper"
)

func TestStopReleasesOutputBeforeSlowServerCleanup(t *testing.T) {
	for _, blockedPath := range []string{"/Sessions/Playing/Stopped", "/UserItems/item/UserData", "/LiveStreams/Close"} {
		t.Run(blockedPath, func(t *testing.T) {
			live := blockedPath == "/LiveStreams/Close"
			kind := "Movie"
			if live {
				kind = "LiveTvChannel"
			}
			player := filepath.Join(t.TempDir(), "player")
			if err := os.WriteFile(player, []byte("#!/bin/sh\nprintf 'ANS_TIME_POSITION=3\\n'\nwhile IFS= read -r command; do :; done\n"), 0700); err != nil {
				t.Fatal(err)
			}
			blocked, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			saved := make(chan int64, 1)
			stopped := make(chan jellyfin.PlayState, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/Items/item":
					fmt.Fprintf(w, `{"Id":"item","Type":%q,"RunTimeTicks":1000000000}`, kind)
				case "/Items/item/PlaybackInfo":
					fmt.Fprint(w, `{"PlaySessionId":"session","MediaSources":[{"Id":"source","LiveStreamId":"tuner","TranscodingUrl":"/stream"}]}`)
				case "/Videos/item/stream", "/stream":
					fmt.Fprint(w, "media")
				case "/Sessions/Playing/Stopped":
					var state jellyfin.PlayState
					json.NewDecoder(r.Body).Decode(&state)
					stopped <- state
				case "/UserItems/item/UserData":
					var state struct{ PlaybackPositionTicks int64 }
					json.NewDecoder(r.Body).Decode(&state)
					saved <- state.PlaybackPositionTicks
				case "/LiveStreams/Close":
					if r.URL.Query().Get("LiveStreamId") != "tuner" {
						t.Error("released the wrong tuner")
					}
				}
				if r.URL.Path == blockedPath {
					close(blocked)
					<-release
				}
			}))
			defer server.Close()
			defer once.Do(func() { close(release) })
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			client := jellyfin.NewClient(jellyfin.Config{Server: server.URL}, jellyfin.Session{})
			fast := make(chan struct{})
			position := make(chan int64, 1)
			released, cleaned := make(chan struct{}), make(chan struct{})
			returned := make(chan error, 1)
			go func() {
				returned <- Run(ctx, client, Config{VideoDecoder: nativeplayer.Decoder{Player: player, Width: 640, Height: 240}, AudioDecoder: nativeplayer.Decoder{Player: player, Width: 640, Height: 240}, Height: 240}, Request{Item: jellyfin.Item{ID: "item", Type: kind}, AsyncCleanup: fast, Callbacks: Callbacks{AcquireVideo: func() {}, ReleaseVideo: func() { close(released) }, CleanupDone: func() { close(cleaned) }, Position: func(ticks int64) { position <- ticks }}})
			}()
			select {
			case <-position:
			case <-time.After(3 * time.Second):
				t.Fatal("decoder did not start")
			}
			close(fast)
			cancel()
			awaitReportSignal(t, blocked)
			select {
			case err := <-returned:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("server cleanup delayed returning to the browser")
			}
			select {
			case <-released:
			default:
				t.Fatal("returned while the decoder still owned video output")
			}
			select {
			case <-cleaned:
				t.Fatal("cleanup completed while its HTTP request was blocked")
			default:
			}
			once.Do(func() { close(release) })
			awaitReportSignal(t, cleaned)
			if state := <-stopped; state.PositionTicks != 30000000 {
				t.Fatalf("lost final position: %d", state.PositionTicks)
			}
			if !live {
				if ticks := <-saved; ticks != 30000000 {
					t.Fatalf("saved stale resume position: %d", ticks)
				}
			}
		})
	}
}

func TestStopCanDetachReportingAlreadyInProgress(t *testing.T) {
	blocked, release := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(blocked)
		<-release
	}))
	defer server.Close()
	defer close(release)
	client := jellyfin.NewClient(jellyfin.Config{Server: server.URL}, jellyfin.Session{})
	r := newProgressReporter(context.Background(), client, false)
	fast, returned := make(chan struct{}), make(chan struct{})
	go func() {
		r.finish(media.PlayState{ItemID: "item"}, false, false, false, fast)
		close(returned)
	}()
	awaitReportSignal(t, blocked)
	close(fast)
	awaitReportSignal(t, returned)
}

func TestCleanupDoneRunsWhenPreparationCannotStart(t *testing.T) {
	calls := 0
	err := Run(context.Background(), nil, Config{VideoDecoder: inlineplayer.Decoder{}, AudioDecoder: inlineplayer.Decoder{}}, Request{Item: jellyfin.Item{Type: "Movie"}, Callbacks: Callbacks{CleanupDone: func() { calls++ }, Position: func(int64) {}}})
	if err == nil || calls != 1 {
		t.Fatalf("early failure did not complete cleanup exactly once: err=%v calls=%d", err, calls)
	}
}
