package plex

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"mistervision/internal/media"
	"mistervision/internal/subtitles"
)

func TestSubtitleSelectionAndBurnIn(t *testing.T) {
	for _, tc := range []struct {
		name, codec string
		index, burn int
		external    bool
	}{{"off", "pgs", -1, -1, false}, {"image", "pgs", 12, 12, false}, {"embedded text", "ass", 12, 12, false}, {"sidecar text", "srt", 12, -1, true}} {
		t.Run(tc.name, func(t *testing.T) {
			var selection, decision url.Values
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/library/parts/8":
					selection = r.URL.Query()
				case "/video/:/transcode/universal/decision":
					decision = r.URL.Query()
					fmt.Fprint(w, `{"MediaContainer":{"generalDecisionCode":1001}}`)
				default:
					t.Errorf("unexpected path %s", r.URL.Path)
				}
			})
			item := metadata{ID: "42", Media: []version{{Part: []part{{ID: "8", Streams: []stream{{ID: 12, Type: 3, Codec: tc.codec}}}}}}}
			if tc.external {
				item.Media[0].Part[0].Streams[0].Key = "/library/streams/12"
			}
			prepared, err := c.PrepareVideo(t.Context(), media.VideoRequest{Item: item.item(), SessionID: "test", SourceID: "0:8", Tracks: media.TrackSelection{AudioIndex: -1, SubtitleIndex: tc.index}, BurnSubtitle: tc.burn})
			if err != nil {
				t.Fatal(err)
			}
			mode, selected := "none", "0"
			if tc.burn >= 0 {
				mode, selected = "burn", "12"
			}
			if selection.Get("subtitleStreamID") != selected || decision.Get("subtitles") != mode {
				t.Fatal("incorrect server subtitle selection")
			}
			u, _ := url.Parse(prepared.URL)
			if u.Query().Get("subtitles") != mode {
				t.Fatal("stream differs from decision")
			}
		})
	}
}

func TestSidecarSubtitleDownload(t *testing.T) {
	downloads := 0
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/library/metadata/42":
			m := metadata{ID: "42", Media: []version{{Part: []part{{ID: "8", Streams: []stream{{ID: 12, Type: 3, Codec: "srt", Key: "/library/streams/12"}, {ID: 13, Type: 3, Codec: "ass"}}}}}}}
			json.NewEncoder(w).Encode(containerResponse{Container: &container{Metadata: []metadata{m}}})
		case "/library/streams/12":
			downloads++
			if r.URL.Query().Get("format") != "srt" || r.URL.Query().Get("encoding") != "utf-8" {
				t.Error("missing subtitle conversion")
			}
			fmt.Fprint(w, "1\n00:00:01,000 --> 00:00:02,000\n♪ Don’t go — ††† ♪\n")
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})
	data, err := c.Subtitle(t.Context(), "42", "0:8", 12)
	if err != nil {
		t.Fatal(err)
	}
	track, err := subtitles.Parse(data)
	if err != nil || track.At(10000000) != "♪ Don’t go — ††† ♪" {
		t.Fatalf("Unicode text changed: %s %v", data, err)
	}
	for _, tc := range []struct {
		source string
		index  int
	}{{"0:9", 12}, {"0:8", 13}, {"0:8", 99}, {"0:8", -1}} {
		if _, err := c.Subtitle(t.Context(), "42", tc.source, tc.index); err == nil {
			t.Fatal("unavailable subtitle downloaded")
		}
	}
	if downloads != 1 {
		t.Fatal("invalid requests reached subtitle endpoint")
	}
}

func TestSubtitleFailureAndSizeLimit(t *testing.T) {
	for _, status := range []int{200, 500} {
		c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/library/metadata/") {
				fmt.Fprint(w, `{"MediaContainer":{"Metadata":[{"ratingKey":"42","Media":[{"Part":[{"id":8,"Stream":[{"id":12,"streamType":3,"codec":"srt","key":"/library/streams/12"}]}]}]}]}}`)
				return
			}
			w.WriteHeader(status)
			fmt.Fprint(w, strings.Repeat("x", (4<<20)+1))
		})
		if _, err := c.Subtitle(t.Context(), "42", "0:8", 12); err == nil {
			t.Fatal("invalid subtitle accepted")
		}
	}
}
