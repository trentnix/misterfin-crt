package plex

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"misterfin-crt/internal/media"
)

func TestMusicHierarchyAndAudioStream(t *testing.T) {
	var timeline bool
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/library/sections/2/all":
			fmt.Fprint(w, `{"MediaContainer":{"totalSize":1,"Metadata":[{"ratingKey":"10","type":"artist","title":"Artist"}]}}`)
		case "/library/metadata/10/children":
			fmt.Fprint(w, `{"MediaContainer":{"totalSize":1,"Metadata":[{"ratingKey":"11","type":"album","title":"Album","parentTitle":"Artist"}]}}`)
		case "/library/metadata/11/children", "/library/metadata/12":
			fmt.Fprint(w, `{"MediaContainer":{"totalSize":1,"Metadata":[{"ratingKey":"12","type":"track","title":"Track","grandparentTitle":"Artist","parentTitle":"Album","duration":60000,"Media":[{"Part":[{"id":8,"key":"/library/parts/8/file.mp3"}]}]}]}}`)
		case "/library/parts/8/file.mp3":
			if r.Header.Get("Range") != "bytes=2-4" {
				t.Error("missing audio range")
			}
			w.Header().Set("Content-Range", "bytes 2-4/6")
			w.WriteHeader(http.StatusPartialContent)
			fmt.Fprint(w, "dio")
		case "/:/timeline":
			timeline = r.URL.Query().Get("duration") == "60000" && r.URL.Query().Get("time") == "12000"
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			w.WriteHeader(404)
		}
	})
	for _, step := range []struct{ id, kind string }{{"library:2", "MusicArtist"}, {"10", "MusicAlbum"}, {"11", "Audio"}} {
		page, err := c.List(t.Context(), media.Location{ParentID: step.id, Collection: "music"}, 0, 64)
		if err != nil || len(page.Items) != 1 || page.Items[0].Type != step.kind {
			t.Fatalf("%s: %+v %v", step.id, page, err)
		}
		item := page.Items[0]
		if item.IsFolder != (step.kind != "Audio") {
			t.Fatal("incorrect folder state")
		}
		if step.kind != "MusicArtist" && (len(item.Artists) != 1 || item.Artists[0] != "Artist") {
			t.Fatal("missing artist")
		}
	}
	tracks, err := c.AudioQueue(t.Context(), media.Location{ParentID: "11"})
	if err != nil || len(tracks) != 1 || tracks[0].Album != "Album" {
		t.Fatalf("queue: %+v %v", tracks, err)
	}
	prepared, err := c.PrepareAudio(t.Context(), tracks[0], "audio-session")
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Release != nil {
		t.Fatal("direct audio allocated a transcode")
	}
	response, err := c.RequestStream(t.Context(), "GET", prepared.URL, http.Header{"Range": {"bytes=2-4"}})
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != 206 || string(body) != "dio" || response.Header.Get("Content-Range") != "bytes 2-4/6" {
		t.Fatal("range response lost")
	}
	err = prepared.Reports.ReportPlaying(t.Context(), "progress", media.PlayState{ItemID: "12", PlaySessionID: "audio-session", PositionTicks: 120000000, Audio: true})
	if err != nil || !timeline {
		t.Fatalf("audio timeline: %v", err)
	}
}

func TestMusicQueuePaginationAndLimit(t *testing.T) {
	for _, total := range []int{205, 10001} {
		t.Run(strconv.Itoa(total), func(t *testing.T) {
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				start, _ := strconv.Atoi(r.URL.Query().Get("X-Plex-Container-Start"))
				entries := []map[string]any{}
				for i := start; i < min(start+200, total); i++ {
					kind := "track"
					if i == 0 {
						kind = "album"
					}
					entries = append(entries, map[string]any{"ratingKey": strconv.Itoa(i + 1), "type": kind})
				}
				json.NewEncoder(w).Encode(map[string]any{"MediaContainer": map[string]any{"totalSize": total, "Metadata": entries}})
			})
			tracks, err := c.AudioQueue(t.Context(), media.Location{ParentID: "11"})
			if total == 205 {
				if err != nil || len(tracks) != 204 || tracks[0].ID != "2" || tracks[203].ID != "205" {
					t.Fatalf("queue: %d %v", len(tracks), err)
				}
			} else if err == nil || tracks != nil {
				t.Fatal("oversized queue accepted")
			}
		})
	}
}

func TestMusicShuffle(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.URL.Path != "/library/sections/2/all" || q.Get("type") != "10" || q.Get("sort") != "random" || q.Get("X-Plex-Container-Size") != "64" {
			t.Error("incorrect shuffle request")
		}
		fmt.Fprint(w, `{"MediaContainer":{"Metadata":[{"ratingKey":"12","type":"track"},{"ratingKey":"12","type":"track"},{"ratingKey":"13","type":"track"}]}}`)
	})
	tracks, err := c.RandomTracks(t.Context(), "library:2")
	if err != nil || len(tracks) != 2 || tracks[1].ID != "13" {
		t.Fatalf("shuffle: %+v %v", tracks, err)
	}
	if _, err := c.RandomTracks(t.Context(), "../2"); err == nil {
		t.Fatal("invalid library accepted")
	}
}

func TestAudioRejectsUnsupportedSources(t *testing.T) {
	for _, parts := range []string{`[]`, `[{"key":"/library/parts/8"},{"key":"/library/parts/9"}]`, `[{"key":"https://other/track"}]`, `[{"key":"/library/parts/8?token=private-token"}]`} {
		t.Run(parts, func(t *testing.T) {
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprintf(w, `{"MediaContainer":{"Metadata":[{"ratingKey":"12","type":"track","Media":[{"Part":%s}]}]}}`, parts)
			})
			_, err := c.PrepareAudio(t.Context(), media.Item{ID: "12"}, "audio-session")
			if err == nil || strings.Contains(err.Error(), "private-token") {
				t.Fatalf("unsafe audio source: %v", err)
			}
		})
	}
}
