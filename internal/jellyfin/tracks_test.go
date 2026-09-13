package jellyfin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestSelectedVideoUsesActualStreamIndexes(t *testing.T) {
	c := NewClient(Config{Server: "http://server"}, Session{Token: "secret"})
	for _, burn := range []int{-1, 12} {
		raw := c.SelectedVideoURL("item", "session", 900000000, true, "source-id", TrackSelection{AudioIndex: 7, SubtitleIndex: 12}, burn)
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		q := u.Query()
		if q.Get("audioStreamIndex") != "7" || q.Get("mediaSourceId") != "source-id" || q.Get("startTimeTicks") != "900000000" {
			t.Fatal("lost stream selection or seek position")
		}
		if burn >= 0 {
			if q.Get("subtitleStreamIndex") != "12" || q.Get("subtitleMethod") != "Encode" {
				t.Fatal("missing burn-in")
			}
		} else if q.Get("subtitleStreamIndex") != "-1" || q.Has("subtitleMethod") {
			t.Fatal("implicit subtitles enabled")
		}
	}
}

func TestSubtitleUsesAuthenticatedExtractionEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/Videos/item/source/Subtitles/12/Stream.srt" || !strings.Contains(r.Header.Get("Authorization"), `Token="secret"`) {
			t.Error("wrong source/index or missing authorization")
		}
		w.Write([]byte("1\n00:00:00,000 --> 00:00:01,000\nHello"))
	}))
	defer server.Close()
	c := NewClient(Config{Server: server.URL}, Session{Token: "secret"})
	if _, err := c.Subtitle(context.Background(), "item", "source", 12); err != nil {
		t.Fatal(err)
	}
}
