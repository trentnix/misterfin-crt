package plex

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"misterfin-crt/internal/media"
)

// Model Plex's session-scoped timeline stop while two streams overlap. A seek
// prepares and opens its replacement before the previous decoder finishes.
func TestSeekReplacementSurvivesPreviousStop(t *testing.T) {
	var mu sync.Mutex
	registered := map[string]bool{}
	stopped := map[string]bool{}
	finish := make(chan struct{})
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		identity := r.Header.Get("X-Plex-Session-Identifier")
		if r.URL.Query().Has("X-Plex-Session-Identifier") {
			t.Error("playback identity belongs in the header")
		}
		switch r.URL.Path {
		case "/library/parts/8":
		case "/video/:/transcode/universal/decision":
			if identity == "" || identity != r.URL.Query().Get("session") {
				t.Error("missing decision identity")
			}
			mu.Lock()
			registered[identity] = true
			mu.Unlock()
			fmt.Fprint(w, `{"MediaContainer":{"generalDecisionCode":1001}}`)
		case "/video/:/transcode/universal/start.mkv":
			mu.Lock()
			known := registered[identity]
			mu.Unlock()
			if !known || identity == "" || identity != r.URL.Query().Get("session") {
				http.Error(w, "unregistered session", 400)
				return
			}
			fmt.Fprint(w, "start")
			w.(http.Flusher).Flush()
			select {
			case <-r.Context().Done():
				return
			case <-finish:
			}
			mu.Lock()
			ended := stopped[identity]
			mu.Unlock()
			if !ended {
				fmt.Fprint(w, "after-stop")
			}
		case "/:/timeline":
			if identity != "old" || r.URL.Query().Get("state") != "stopped" {
				t.Error("old stop lost its identity")
			}
			mu.Lock()
			stopped[identity] = true
			mu.Unlock()
		case "/video/:/transcode/universal/stop":
			if identity != r.URL.Query().Get("session") {
				t.Error("release lost its identity")
			}
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(404)
		}
	})
	prepare := func(id string, offset int64) (media.PreparedStream, io.ReadCloser) {
		t.Helper()
		p, err := c.PrepareVideo(t.Context(), media.VideoRequest{Item: media.Item{ID: "42", MediaSources: []media.MediaSource{{ID: "0:8"}}}, SessionID: id, StartTicks: offset, SourceID: "0:8", Tracks: media.TrackSelection{AudioIndex: -1, SubtitleIndex: -1}, BurnSubtitle: -1})
		if err != nil {
			t.Fatal(err)
		}
		body, err := c.OpenStream(t.Context(), p.URL)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { body.Close() })
		if _, err := io.ReadFull(body, make([]byte, 5)); err != nil {
			t.Fatal(err)
		}
		return p, body
	}
	old, body := prepare("old", 0)
	_, replacement := prepare("new", 300000000)
	body.Close()
	if err := old.Reports.ReportPlaying(t.Context(), "stopped", media.PlayState{ItemID: "42", PlaySessionID: "old"}); err != nil {
		t.Fatal(err)
	}
	if err := old.Release(t.Context()); err != nil {
		t.Fatal(err)
	}
	close(finish)
	data, err := io.ReadAll(replacement)
	if err != nil || string(data) != "after-stop" {
		t.Fatalf("replacement interrupted: %q %v", data, err)
	}
}

func TestRejectedTranscodeDecision(t *testing.T) {
	for _, response := range []string{`{}`, `{"MediaContainer":{}}`, `{"MediaContainer":{"generalDecisionCode":2000,"generalDecisionText":"private-token"}}`, `invalid private-token`} {
		t.Run(response, func(t *testing.T) {
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/decision") {
					fmt.Fprint(w, response)
				} else if r.URL.Path != "/library/parts/8" {
					t.Error("opened rejected stream")
				}
			})
			_, err := c.PrepareVideo(t.Context(), media.VideoRequest{Item: media.Item{ID: "42", MediaSources: []media.MediaSource{{ID: "0:8"}}}, SessionID: "test", SourceID: "0:8", Tracks: media.TrackSelection{AudioIndex: -1, SubtitleIndex: -1}, BurnSubtitle: -1})
			if err == nil || strings.Contains(err.Error(), "private-token") {
				t.Fatalf("unsafe decision error: %v", err)
			}
		})
	}
}
