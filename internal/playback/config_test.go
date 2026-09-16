package playback

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"misterfin-crt/internal/jellyfin"
	"misterfin-crt/internal/media"
	nativeplayer "misterfin-crt/internal/player/mplayer"
)

func TestConfigReuseKeepsRequestsIndependent(t *testing.T) {
	queries := make(chan url.Values, 3)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/Items/"):
			fmt.Fprintf(w, `{"Id":%q,"Type":"Movie","RunTimeTicks":9000000000,"UserData":{"PlaybackPositionTicks":600000000},"MediaStreams":[{"Type":"Video","Width":1920,"Height":1080,"AspectRatio":"16:9"}]}`, strings.TrimPrefix(r.URL.Path, "/Items/"))
		case strings.HasPrefix(r.URL.Path, "/Videos/"):
			queries <- r.URL.Query()
			fmt.Fprint(w, "video")
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()
	client := jellyfin.NewClient(jellyfin.Config{Server: server.URL}, jellyfin.Session{UserID: "user"})
	dir := t.TempDir()
	player := filepath.Join(dir, "player")
	argsFile := filepath.Join(dir, "args")
	t.Setenv("PLAYBACK_TEST_ARGS", argsFile)
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$PLAYBACK_TEST_ARGS\"\ncat /dev/fd/3 >/dev/null\nprintf 'ANS_TIME_POSITION=1\\n'\n"
	if err := os.WriteFile(player, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	preferences := NewPreferences(dir, nil)
	defer preferences.Close()
	config := Config{Preferences: preferences, VideoDecoder: nativeplayer.Decoder{Player: player, Width: 640, Height: 240}, Height: 240, AudioDecoder: nativeplayer.Decoder{Width: 640, Height: 240}}
	originalConfig := config
	item := jellyfin.Item{ID: "movie", Type: "Movie"}
	explicit := TrackOptions{Picture: PictureZoom43, Selection: media.TrackSelection{AudioIndex: -1, SubtitleIndex: -1}}
	originalTracks := explicit
	var zero int64
	positions := 0
	requests := []Request{
		{Item: item, Tracks: &explicit, StartTicks: &zero, Callbacks: Callbacks{Position: func(int64) { positions++ }}},
		{Item: jellyfin.Item{ID: "other", Type: "Movie"}}, // A different item uses its own defaults.
		{Item: item}, // Reopening the first item restores its saved Zoom, without restarting.
	}
	for i, request := range requests {
		before, _ := json.Marshal(request.Item)
		previousPositions := positions
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := Run(ctx, client, config, request)
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		select {
		case query := <-queries:
			wantStart := "600000000"
			if i == 0 {
				wantStart = "0"
			}
			if got := query.Get("startTimeTicks"); got != wantStart {
				t.Fatalf("request %d start = %s, want %s", i, got, wantStart)
			}
		default:
			t.Fatal("missing stream request")
		}
		args, err := os.ReadFile(argsFile)
		wantPicture := "misterfin=640:240:1.777777778:1"
		if i == 1 {
			wantPicture = "misterfin=640:240:1.777777778:0"
		}
		if err != nil || !strings.Contains(string(args), wantPicture) {
			t.Fatalf("request %d decoder picture mismatch: %s (%v)", i, args, err)
		}
		after, _ := json.Marshal(request.Item)
		if config != originalConfig || !reflect.DeepEqual(explicit, originalTracks) || zero != 0 || string(before) != string(after) {
			t.Fatal("playback mutated caller-owned configuration or request data")
		}
		if i == 0 && positions == 0 {
			t.Fatal("first request did not receive position feedback")
		}
		if i > 0 && positions != previousPositions {
			t.Fatal("position callback leaked into a later request")
		}
	}
}
