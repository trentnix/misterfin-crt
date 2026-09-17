package jellyfin

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestCollectionAndPlaylistQueries(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("userId") != "user" {
			t.Error("user scope missing")
		}
		switch r.URL.Path {
		case "/UserViews":
			fmt.Fprint(w, `{"Items":[{"Id":"sets","Name":"My Collections","CollectionType":"boxsets"}]}`)
		case "/LiveTv/Channels":
			fmt.Fprint(w, `{"Items":[],"TotalRecordCount":0}`)
		case "/Items":
			if q.Get("ParentId") == "set" {
				if q.Has("Recursive") || q.Has("IncludeItemTypes") || q.Has("SortBy") {
					t.Error("collection inherited library filtering or sorting")
				}
				fmt.Fprint(w, `{"Items":[{"Id":"series","Type":"Series"},{"Id":"movie","Type":"Movie"}],"TotalRecordCount":2}`)
			} else {
				if q.Has("ParentId") || q.Get("Recursive") != "true" {
					t.Error("global container discovery used a synthetic parent")
				}
				kind := q.Get("IncludeItemTypes")
				if kind != "BoxSet" && kind != "Playlist" {
					t.Errorf("unexpected query %v", q)
				}
				fmt.Fprintf(w, `{"Items":[{"Id":"container","Type":%q}],"TotalRecordCount":1}`, kind)
			}
		case "/Playlists/list/Items":
			if q.Has("SortBy") || q.Has("ParentId") || q.Get("StartIndex") != "64" || q.Get("Limit") != "64" {
				t.Error("playlist query changed order or page")
			}
			fmt.Fprint(w, `{"Items":[{"Id":"b","Type":"Audio"},{"Id":"a","Type":"Audio"},{"Id":"b","Type":"Audio"}],"TotalRecordCount":67}`)
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	c := NewClient(Config{Server: server.URL}, Session{UserID: "user"})
	libraries, err := c.Libraries(t.Context())
	if err != nil || len(libraries.Items) != 2 || libraries.Items[0].Name != "My Collections" || libraries.Items[1].CollectionType != "playlists" {
		t.Fatalf("libraries: %+v %v", libraries, err)
	}
	for _, card := range libraries.Items {
		count, err := c.LibraryCount(t.Context(), card)
		if err != nil || count == nil || *count != 1 {
			t.Fatalf("count: %v %v", count, err)
		}
		page, err := c.Mosaic(t.Context(), card)
		if err != nil || len(page.Items) != 1 {
			t.Fatalf("mosaic: %+v %v", page, err)
		}
	}
	page, err := c.List(t.Context(), Location{Kind: "collection", ParentID: "set", Collection: "movies"}, 0, 64)
	if err != nil || len(page.Items) != 2 || page.Items[0].Type != "Series" {
		t.Fatalf("collection: %+v %v", page, err)
	}
	page, err = c.List(t.Context(), Location{Kind: "playlist", ParentID: "list", Collection: "music"}, 64, 64)
	if err != nil || len(page.Items) != 3 || page.Items[0].ID != "b" || page.Items[1].ID != "a" || page.Items[2].ID != "b" {
		t.Fatalf("playlist order: %+v %v", page, err)
	}
}

func TestUnavailableOrganizationsPreserveJellyfinLibraries(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/UserViews" {
			fmt.Fprint(w, `{"Items":[{"Id":"movies","CollectionType":"movies"}]}`)
			return
		}
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()
	c := NewClient(Config{Server: server.URL}, Session{UserID: "user"})
	page, err := c.Libraries(t.Context())
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("libraries lost: %+v %v", page, err)
	}
}

func TestEmptyServerSuppliedOrganizationCardsAreHidden(t *testing.T) {
	for _, status := range []int{200, 403, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var probes atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/UserViews":
					fmt.Fprint(w, `{"Items":[{"Id":"movies","CollectionType":"movies"},{"Id":"sets","Name":"My Collections","CollectionType":"boxsets","ChildCount":99},{"Id":"lists","Name":"My Playlists","CollectionType":"playlists","ChildCount":99}]}`)
				case "/Items":
					probes.Add(1)
					if status != 200 {
						w.WriteHeader(status)
						return
					}
					fmt.Fprint(w, `{"Items":[],"TotalRecordCount":0}`)
				case "/LiveTv/Channels":
					fmt.Fprint(w, `{"Items":[],"TotalRecordCount":0}`)
				default:
					t.Errorf("unexpected request %s", r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			defer server.Close()
			c := NewClient(Config{Server: server.URL}, Session{UserID: "user"})
			page, err := c.Libraries(t.Context())
			if err != nil || len(page.Items) != 1 || page.Items[0].ID != "movies" || *page.TotalRecordCount != 1 {
				t.Fatalf("empty cards retained: %+v %v", page, err)
			}
			if probes.Load() != 2 {
				t.Fatal("hidden cards were probed again as synthetic cards")
			}
		})
	}
}
