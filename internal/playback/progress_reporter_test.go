package playback

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"misterfin-crt/internal/jellyfin"
)

func awaitReportSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(3 * time.Second):
		t.Fatal("reporting worker did not reach expected state")
	}
}

func TestProgressReportsCoalesceAndFinishInOrder(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	latest := make(chan struct{})
	savedLatest := make(chan struct{})
	var mu sync.Mutex
	var events []string
	var states []jellyfin.PlayState
	var saved []int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var state jellyfin.PlayState
		if r.URL.Path == "/UserItems/item/UserData" {
			var data struct{ PlaybackPositionTicks int64 }
			json.NewDecoder(r.Body).Decode(&data)
			mu.Lock()
			events = append(events, "save")
			saved = append(saved, data.PlaybackPositionTicks)
			mu.Unlock()
			if data.PlaybackPositionTicks == 100 {
				close(savedLatest)
			}
			return
		}
		json.NewDecoder(r.Body).Decode(&state)
		mu.Lock()
		events = append(events, r.URL.Path)
		states = append(states, state)
		mu.Unlock()
		if r.URL.Path == "/Sessions/Playing" {
			close(started)
			<-release
		}
		if r.URL.Path == "/Sessions/Playing/Progress" && state.PositionTicks == 100 {
			close(latest)
		}
	}))
	defer server.Close()
	defer once.Do(func() { close(release) })
	client := jellyfin.NewClient(jellyfin.Config{Server: server.URL}, jellyfin.Session{})
	reporter := newProgressReporter(context.Background(), client, false)
	canSeek := true
	state := jellyfin.PlayState{ItemID: "item", PlaySessionID: "session", CanSeek: &canSeek}
	reporter.start(state)
	awaitReportSignal(t, started)
	for i := int64(1); i <= 100; i++ {
		state.PositionTicks = i
		state.IsPaused = i == 100
		reporter.progress(state, i == 1, false)
	}
	canSeek = false
	reporter.mu.Lock()
	pending := reporter.pending
	reporter.mu.Unlock()
	if pending == nil || pending.state.PositionTicks != 100 || !pending.save {
		t.Fatal("pending reports were not coalesced with save intent")
	}
	once.Do(func() { close(release) })
	awaitReportSignal(t, latest)
	awaitReportSignal(t, savedLatest)
	state.PositionTicks = 101
	if reporter.finish(state, true, false, false, nil) {
		t.Fatal("unexpected reporting failure")
	}
	reporter.progress(state, true, false)
	mu.Lock()
	defer mu.Unlock()
	want := []string{"/Sessions/Playing", "/Sessions/Playing/Progress", "/Sessions/Playing/Progress", "save", "/Sessions/Playing/Stopped", "save"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("report order: %v", events)
	}
	if !reflect.DeepEqual(saved, []int64{100, 101}) {
		t.Fatalf("saved positions: %v", saved)
	}
	if states[0].PositionTicks != 0 || !*states[0].CanSeek || !*states[2].CanSeek || !states[2].IsPaused {
		t.Fatal("queued snapshots changed with producer state")
	}
}

func TestSlowReportingDoesNotBlockDecoderControlsOrFeedback(t *testing.T) {
	player := filepath.Join(t.TempDir(), "player")
	script := `#!/bin/sh
printf 'ANS_TIME_POSITION=0\n'
position=0
while IFS= read -r command; do
 if [ "$command" = pause ]; then
  position=$((position + 1))
  printf 'ANS_BUFFERING=false\nANS_TIME_POSITION=%s\n' "$position"
 fi
done
`
	if err := os.WriteFile(player, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	stopped := make(chan jellyfin.PlayState, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/Items/movie":
			fmt.Fprint(w, `{"Id":"movie","Type":"Movie","RunTimeTicks":1000000000}`)
		case "/Videos/movie/stream":
			fmt.Fprint(w, "media")
		case "/Sessions/Playing":
			var state jellyfin.PlayState
			json.NewDecoder(r.Body).Decode(&state)
			close(started)
			<-r.Context().Done()
		case "/Sessions/Playing/Stopped":
			var state jellyfin.PlayState
			json.NewDecoder(r.Body).Decode(&state)
			stopped <- state
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := jellyfin.NewClient(jellyfin.Config{Server: server.URL}, jellyfin.Session{})
	controls := make(chan Control, 2)
	paused := make(chan bool, 2)
	positions := make(chan int64, 8)
	buffering := make(chan bool, 8)
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, client, Config{VideoDecoder: DecoderConfig{Kind: DecoderMPlayer, Player: player}, AudioDecoder: DecoderConfig{Kind: DecoderMPlayer, Player: player}, Width: 640, Height: 240, Device: "/dev/fb0"}, Request{Item: jellyfin.Item{ID: "movie", Type: "Movie"}, Controls: controls, Callbacks: Callbacks{Paused: func(value bool) { paused <- value }, Buffering: func(value bool) { buffering <- value }, Position: func(ticks int64) { positions <- ticks }}})
	}()
	awaitReportSignal(t, started)
	for _, want := range []bool{true, false} {
		controls <- Control{Kind: TogglePause}
		select {
		case got := <-paused:
			if got != want {
				t.Fatal("incorrect pause state")
			}
		case <-time.After(time.Second):
			t.Fatal("slow reporting blocked pause/resume")
		}
	}
	select {
	case <-buffering:
	case <-time.After(time.Second):
		t.Fatal("slow reporting blocked decoder feedback")
	}
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	for {
		select {
		case ticks := <-positions:
			if ticks >= 20000000 {
				goto advanced
			}
		case <-timer.C:
			t.Fatal("slow reporting blocked position updates")
		}
	}
advanced:
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled routine report delayed final cleanup")
	}
	select {
	case state := <-stopped:
		if state.PositionTicks != 20000000 || state.IsPaused {
			t.Fatalf("stale final state: %+v", state)
		}
	default:
		t.Fatal("stop report was not delivered")
	}
}

func TestReportingFailuresAreRememberedButFinalErrorsRemainBestEffort(t *testing.T) {
	saved := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/UserItems/item/UserData" {
			saved <- struct{}{}
		}
		if r.URL.Path != "/Sessions/Playing" {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
		}
	}))
	defer server.Close()
	client := jellyfin.NewClient(jellyfin.Config{Server: server.URL}, jellyfin.Session{})
	state := jellyfin.PlayState{ItemID: "item", PlaySessionID: "session"}
	reporter := newProgressReporter(context.Background(), client, false)
	reporter.start(state)
	reporter.progress(state, true, false)
	awaitReportSignal(t, saved)
	if !reporter.finish(state, true, false, false, nil) {
		t.Fatal("routine reporting failures were lost")
	}
	// A session that never reached playback still releases its transcode. Errors
	// from that final best-effort request must not replace its original outcome.
	reporter = newProgressReporter(context.Background(), client, false)
	if reporter.finish(state, false, false, false, nil) {
		t.Fatal("final-only error changed the playback outcome")
	}
}

func TestAsyncReportCleanupOwnsFinalSnapshotAndWaitsForStopBeforeSave(t *testing.T) {
	stopStarted, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	saved := make(chan int64, 1)
	stopState := make(chan jellyfin.PlayState, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/Sessions/Playing/Stopped" {
			var state jellyfin.PlayState
			json.NewDecoder(r.Body).Decode(&state)
			stopState <- state
			close(stopStarted)
			<-release
		} else if r.URL.Path == "/UserItems/item/UserData" {
			var state struct{ PlaybackPositionTicks int64 }
			json.NewDecoder(r.Body).Decode(&state)
			saved <- state.PlaybackPositionTicks
		}
	}))
	defer server.Close()
	defer once.Do(func() { close(release) })
	reporter := newProgressReporter(context.Background(), jellyfin.NewClient(jellyfin.Config{Server: server.URL}, jellyfin.Session{}), false)
	seekable := true
	state := jellyfin.PlayState{ItemID: "item", PlaySessionID: "session", PositionTicks: 42, CanSeek: &seekable}
	async := make(chan struct{})
	close(async)
	returned := make(chan struct{})
	go func() { reporter.finish(state, true, false, false, async); close(returned) }()
	awaitReportSignal(t, returned)
	state.PositionTicks = 99
	seekable = false
	awaitReportSignal(t, stopStarted)
	if got := <-stopState; got.PositionTicks != 42 || !*got.CanSeek {
		t.Fatalf("final snapshot changed after handoff: %+v", got)
	}
	select {
	case <-saved:
		t.Fatal("resume save overtook stop")
	default:
	}
	once.Do(func() { close(release) })
	awaitReportSignal(t, reporter.done)
	if got := <-saved; got != 42 {
		t.Fatalf("saved mutated final position: %d", got)
	}
}

func TestFastCompletionPreservesInitialReportAndSurfacesReportingFailure(t *testing.T) {
	player := filepath.Join(t.TempDir(), "player")
	if err := os.WriteFile(player, []byte("#!/bin/sh\nprintf 'ANS_TIME_POSITION=1\\n'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var events []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/Items/movie":
			fmt.Fprint(w, `{"Id":"movie","Type":"Movie"}`)
		case "/Videos/movie/stream":
			fmt.Fprint(w, "media")
		default:
			mu.Lock()
			events = append(events, r.URL.Path)
			mu.Unlock()
			if r.URL.Path == "/Sessions/Playing/Progress" {
				http.Error(w, "unavailable", http.StatusServiceUnavailable)
			}
		}
	}))
	defer server.Close()
	client := jellyfin.NewClient(jellyfin.Config{Server: server.URL}, jellyfin.Session{})
	err := Run(context.Background(), client, Config{VideoDecoder: DecoderConfig{Kind: DecoderFFplay, Player: player}, AudioDecoder: DecoderConfig{Kind: DecoderFFplay, Player: player}}, Request{Item: jellyfin.Item{ID: "movie", Type: "Movie"}, Callbacks: Callbacks{Position: func(int64) {}}})
	if err == nil || err.Error() != "playback ended, but Jellyfin progress reporting failed" {
		t.Fatalf("reporting failure was lost at EOF: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	want := []string{"/Sessions/Playing", "/Sessions/Playing/Progress", "/Sessions/Playing/Stopped", "/UserItems/movie/UserData"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("fast completion lost reporting order: %v", events)
	}
}
