package playback

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"misterfin-crt/internal/jellyfin"
)

func TestTrackPreparationChoosesSourceAndBurnIn(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Query().Get("Fields"), "MediaSources") {
			t.Error("missing source metadata query")
		}
		json.NewEncoder(w).Encode(jellyfin.Item{ID: "movie", Type: "Movie", RunTimeTicks: 1000000000, MediaSources: []jellyfin.MediaSource{{ID: "file-source", MediaStreams: []jellyfin.MediaStream{{Type: "Audio", Index: 4}, {Type: "Subtitle", Index: 12, Codec: "ass"}, {Type: "Subtitle", Index: 18, Codec: "pgssub"}}}}})
	}))
	defer server.Close()
	client := jellyfin.NewClient(jellyfin.Config{Server: server.URL}, jellyfin.Session{})
	for _, tc := range []struct {
		index    int
		burnText bool
		burn     string
	}{{12, false, "-1"}, {12, true, "12"}, {18, false, "18"}, {-1, false, "-1"}} {
		start := int64(50000000)
		session, err := preparePlayback(context.Background(), client, jellyfin.Item{ID: "movie", Type: "Movie"}, Options{Height: 240, StartTicks: &start, burnText: tc.burnText, Tracks: &TrackOptions{Selection: jellyfin.TrackSelection{AudioIndex: 4, SubtitleIndex: tc.index}}})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(session.streamURL, "subtitleStreamIndex="+tc.burn) || !strings.Contains(session.streamURL, "audioStreamIndex=4") || !strings.Contains(session.streamURL, "mediaSourceId=file-source") || session.start != start {
			t.Fatal("wrong source, track, or offset")
		}
	}
	_, err := preparePlayback(context.Background(), client, jellyfin.Item{ID: "movie"}, Options{Tracks: &TrackOptions{Selection: jellyfin.TrackSelection{AudioIndex: 99, SubtitleIndex: -1}}})
	if err == nil {
		t.Fatal("missing track silently fell back")
	}
}

func TestSubtitleLoaderCancellationAndFailure(t *testing.T) {
	started := make(chan struct{})
	canceled := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/12/") {
			close(started)
			<-r.Context().Done()
			close(canceled)
			return
		}
		http.Error(w, "private server diagnostic", http.StatusInternalServerError)
	}))
	defer server.Close()
	client := jellyfin.NewClient(jellyfin.Config{Server: server.URL}, jellyfin.Session{})
	loader := subtitleLoader{results: make(chan SubtitleResult, 1)}
	defer loader.stop()
	loader.start(context.Background(), client, "movie", "source", 12, 1)
	<-started
	loader.start(context.Background(), client, "movie", "source", -1, 2)
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("extraction was not canceled")
	}
	for {
		result := <-loader.results
		if result.serial != loader.serial {
			continue
		}
		if result.Index != -1 || result.Text != nil || result.Err != nil {
			t.Fatal(result)
		}
		break
	}
	loader.start(context.Background(), client, "movie", "source", 20, 3)
	result := <-loader.results
	if result.Err == nil || strings.Contains(result.Err.Error(), "private") {
		t.Fatal("failed extraction leaked diagnostic or appeared successful")
	}
}
