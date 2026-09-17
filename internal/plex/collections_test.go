package plex

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"testing"

	"misterfin-crt/internal/media"
)

func TestCollectionsAndPlaylistsUseDedicatedEndpoints(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch r.URL.Path {
		case "/library/sections":
			fmt.Fprint(w, `{"MediaContainer":{"Directory":[]}}`)
		case "/livetv/dvrs":
			fmt.Fprint(w, `{"MediaContainer":{"Dvr":[]}}`)
		case "/library/all":
			if q.Get("type") != "18" {
				t.Error("collections must filter type 18")
			}
			fmt.Fprint(w, `{"MediaContainer":{"totalSize":1,"Metadata":[{"ratingKey":"10","type":"collection","title":"Favorites"}]}}`)
		case "/playlists":
			if q.Get("type") != "15" || q.Get("playlistType") != "audio,video,photo" {
				t.Error("missing flat playlist filter")
			}
			fmt.Fprint(w, `{"MediaContainer":{"totalSize":1,"Metadata":[{"ratingKey":"20","type":"playlist","title":"Mix","composite":"/playlists/20/composite/123"}]}}`)
		case "/library/collections/10/items":
			if q.Has("sort") {
				t.Error("collection order replaced")
			}
			fmt.Fprint(w, `{"MediaContainer":{"totalSize":2,"Metadata":[{"ratingKey":"21","type":"show"},{"ratingKey":"22","type":"movie"}]}}`)
		case "/playlists/20/items":
			if q.Has("sort") || q.Get("X-Plex-Container-Start") != "64" || q.Get("X-Plex-Container-Size") != "64" {
				t.Error("playlist order or pagination changed")
			}
			fmt.Fprint(w, `{"MediaContainer":{"offset":64,"totalSize":67,"Metadata":[{"ratingKey":"3","type":"track"},{"ratingKey":"2","type":"track"},{"ratingKey":"3","type":"track"}]}}`)
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
			w.WriteHeader(404)
		}
	})
	libraries, err := c.Libraries(t.Context())
	if err != nil || len(libraries.Items) != 2 {
		t.Fatalf("libraries: %+v %v", libraries, err)
	}
	for i, kind := range []string{"BoxSet", "Playlist"} {
		card := libraries.Items[i]
		page, err := c.Mosaic(t.Context(), card)
		count, countErr := c.LibraryCount(t.Context(), card)
		if err != nil || countErr != nil || count == nil || *count != 1 || page.Items[0].Type != kind || !page.Items[0].IsFolder {
			t.Fatalf("card %s: %+v %v %v", kind, page, err, countErr)
		}
		if kind == "Playlist" && page.Items[0].ImageTags["Primary"] == "" {
			t.Fatal("playlist artwork lost")
		}
	}
	page, err := c.List(t.Context(), media.Location{Kind: "collection", ParentID: "10"}, 0, 64)
	if err != nil || page.Items[0].Type != "Series" || page.Items[1].Type != "Movie" {
		t.Fatalf("collection: %+v %v", page, err)
	}
	page, err = c.List(t.Context(), media.Location{Kind: "playlist", ParentID: "20"}, 64, 64)
	if err != nil || len(page.Items) != 3 || page.Items[0].ID != "3" || page.Items[1].ID != "2" || page.Items[2].ID != "3" || *page.TotalRecordCount != 67 {
		t.Fatalf("ordered playlist: %+v %v", page, err)
	}
}

func TestPlaylistAudioQueuePreservesOrderAcrossPages(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/playlists/9/items" {
			t.Fatalf("wrong endpoint %s", r.URL.Path)
		}
		start, _ := strconv.Atoi(r.URL.Query().Get("X-Plex-Container-Start"))
		total := 201
		entries := make([]metadata, min(200, total-start))
		for i := range entries {
			entries[i] = metadata{ID: identifier(strconv.Itoa((start+i)%3 + 1)), Type: "track"}
		}
		json.NewEncoder(w).Encode(containerResponse{Container: &container{Metadata: entries, Total: &total, Offset: start}})
	})
	items, err := c.AudioQueue(t.Context(), media.Location{Kind: "playlist", ParentID: "9"})
	if err != nil || len(items) != 201 || items[199].ID != "2" || items[200].ID != "3" {
		t.Fatalf("queue count=%d error=%v", len(items), err)
	}
}

func TestUnavailableOrganizationsDoNotHideLibraries(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/library/sections" {
			fmt.Fprint(w, `{"MediaContainer":{"Directory":[{"key":"1","title":"Movies","type":"movie"}]}}`)
			return
		}
		w.WriteHeader(http.StatusForbidden)
	})
	page, err := c.Libraries(t.Context())
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("libraries lost: %+v %v", page, err)
	}
}

func TestEmptyOrganizationCatalogsAreHidden(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/library/sections":
			fmt.Fprint(w, `{"MediaContainer":{"Directory":[{"key":"1","title":"Movies","type":"movie"}]}}`)
		case "/library/all", "/playlists":
			fmt.Fprint(w, `{"MediaContainer":{"Metadata":[],"totalSize":0}}`)
		case "/livetv/dvrs":
			fmt.Fprint(w, `{"MediaContainer":{"Dvr":[]}}`)
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			w.WriteHeader(404)
		}
	})
	page, err := c.Libraries(t.Context())
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != "library:1" || *page.TotalRecordCount != 1 {
		t.Fatalf("empty cards retained: %+v %v", page, err)
	}
}

func TestContainerViewsAreNotPlaybackProgress(t *testing.T) {
	for _, kind := range []string{"playlist", "collection", "show", "season", "artist", "album", "photoalbum"} {
		for _, offset := range []int64{0, 12345} {
			item := (metadata{Type: kind, ViewCount: 1, ViewOffset: offset, LastViewedAt: 100, LeafCount: 12, Title: "Fresh ❤️"}).item()
			if item.UserData.Played || item.UserData.PlaybackPositionTicks != 0 {
				t.Errorf("%s: container has playback progress: %+v", kind, item.UserData)
			}
			if item.Name != "Fresh ❤️" || item.UserData.LastPlayedDate == nil {
				t.Errorf("%s: title or last-viewed date lost", kind)
			}
			if kind == "playlist" && item.ChildCount != 12 {
				t.Errorf("playlist item count = %d", item.ChildCount)
			}
		}
	}
	for _, kind := range []string{"movie", "episode", "clip", "track"} {
		for _, offset := range []int64{0, 12345} {
			item := (metadata{Type: kind, ViewCount: 1, ViewOffset: offset}).item()
			if item.UserData.Played != (offset == 0) || item.UserData.PlaybackPositionTicks != offset*10000 {
				t.Errorf("%s: playback progress changed: %+v", kind, item.UserData)
			}
		}
	}
}
