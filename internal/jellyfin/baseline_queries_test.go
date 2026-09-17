package jellyfin

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"
)

func TestCountAndCoverQueriesMatchCBaseline(t *testing.T) {
	for _, tc := range []struct{ collection, kind string }{
		{"movies", "Movie"}, {"tvshows", "Series"}, {"music", "MusicAlbum"}, {"musicvideos", "MusicVideo"},
		{"homevideos", "Video,Photo"}, {"mixed", "Movie,Series,Video,MusicVideo,Audio,Photo"}, {"books", ""},
	} {
		t.Run(tc.collection, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				want := url.Values{"userId": {"user"}, "ParentId": {"library"}, "Recursive": {"true"}, "Limit": {"0"}}
				if tc.kind != "" {
					want.Set("IncludeItemTypes", tc.kind)
				}
				if calls == 2 {
					want.Set("Limit", "12")
					want.Set("SortBy", "SortName")
					want.Set("SortOrder", "Ascending")
					want.Set("Fields", "ProductionYear,RunTimeTicks")
					want.Set("EnableUserData", "true")
					want.Set("ImageTypeLimit", "1")
					want.Set("EnableImageTypes", "Primary")
				}
				if r.URL.Path != "/Items" || !reflect.DeepEqual(r.URL.Query(), want) {
					t.Errorf("request %s, want %v", r.URL, want)
				}
				if calls == 1 {
					fmt.Fprint(w, `{"Items":[],"TotalRecordCount":503}`)
				} else {
					fmt.Fprint(w, `{"Items":[],"TotalRecordCount":12}`)
				}
			}))
			defer server.Close()
			c := NewClient(Config{Server: server.URL}, Session{UserID: "user"})
			item := Item{ID: "library", CollectionType: tc.collection}
			count, err := c.LibraryCount(context.Background(), item)
			if err != nil || count == nil || *count != 503 {
				t.Fatalf("count: %v %v", count, err)
			}
			if _, err = c.Mosaic(context.Background(), item); err != nil {
				t.Fatal(err)
			}
			if calls != 2 {
				t.Fatal("count and covers must use separate requests")
			}
		})
	}
}

func TestLiveTVQueriesAndLibraryNames(t *testing.T) {
	for _, existing := range []bool{true, false} {
		t.Run(fmt.Sprint(existing), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/UserViews" {
					if r.URL.RawQuery != "userId=user" {
						t.Errorf("views query: %s", r.URL)
					}
					if existing {
						fmt.Fprint(w, `{"Items":[{"Id":"cinema","Name":"Family Cinema","CollectionType":"movies"},{"Id":"sports","Name":"Local Broadcasts","CollectionType":"livetv"}]}`)
					} else {
						fmt.Fprint(w, `{"Items":[{"Id":"cinema","Name":"Family Cinema","CollectionType":"movies"}]}`)
					}
					return
				}
				if r.URL.Path == "/Items" {
					fmt.Fprint(w, `{"Items":[],"TotalRecordCount":0}`)
					return
				}
				calls++
				limit := "64"
				start := "64"
				if !existing && calls == 1 {
					limit = "1"
					start = "0"
				}
				want := url.Values{"userId": {"user"}, "StartIndex": {start}, "Limit": {limit}, "AddCurrentProgram": {"true"}, "EnableImages": {"true"}, "ImageTypeLimit": {"1"}, "EnableImageTypes": {"Primary"}}
				if r.URL.Path != "/LiveTv/Channels" || !reflect.DeepEqual(r.URL.Query(), want) {
					t.Errorf("channels query: %s want %v", r.URL, want)
				}
				fmt.Fprint(w, `{"Items":[{"Id":"b","Name":"News","Type":"Channel","Number":"12.2","CurrentProgram":{"Name":"Evening News"}},{"Id":"a","Name":"Arts","ChannelNumber":"3.1"}],"TotalRecordCount":66}`)
			}))
			defer server.Close()
			c := NewClient(Config{Server: server.URL}, Session{UserID: "user"})
			page, err := c.Libraries(context.Background())
			if err != nil || len(page.Items) != 2 || page.Items[0].Name != "Family Cinema" {
				t.Fatalf("libraries: %+v %v", page, err)
			}
			if existing && (page.Items[1].Name != "Local Broadcasts" || calls != 0) {
				t.Fatal("renamed or reprobed server Live TV view")
			}
			if !existing && (page.Items[1].Name != "Live TV" || calls != 1) {
				t.Fatal("missing probed Live TV view")
			}
			count, err := c.LibraryCount(context.Background(), page.Items[1])
			if count != nil || err != nil {
				t.Fatal("Live TV must not use Items counts")
			}
			channels, err := c.List(context.Background(), Location{Kind: "livetv"}, 64, 64)
			if err != nil || len(channels.Items) != 2 || channels.Items[0].ID != "b" || channels.Items[1].ID != "a" || channels.Items[0].Type != "TvChannel" || channels.Items[0].Number != "12.2" || channels.Items[1].ChannelNumber != "3.1" || channels.Items[0].CurrentProgram.Name != "Evening News" {
				t.Fatalf("channels: %+v %v", channels, err)
			}
		})
	}
}

func TestCountFailuresStayUnknown(t *testing.T) {
	for _, body := range []string{`{}`, `{"TotalRecordCount":-1}`, `not json`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
		count, err := NewClient(Config{Server: server.URL}, Session{}).LibraryCount(context.Background(), Item{ID: "library", CollectionType: "movies"})
		server.Close()
		if count != nil || err == nil {
			t.Fatalf("invented count for %s", body)
		}
	}
}

func TestSeasonEpisodeAndDetailsQueriesMatchC(t *testing.T) {
	for _, tc := range []struct{ kind, path, query string }{
		{"seasons", "/Shows/series/Seasons", "userId=user"},
		{"episodes", "/Shows/series/Episodes", "seasonId=season&userId=user&Fields=RunTimeTicks&EnableUserData=true&ImageTypeLimit=1&EnableImageTypes=Primary,Backdrop&StartIndex=64&Limit=64"},
		{"details", "/Items/movie", "userId=user&Fields=Overview,ProductionYear,RunTimeTicks,People,MediaStreams,CommunityRating&EnableUserData=true&EnableImageTypes=Primary,Logo,Backdrop"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				want, _ := url.ParseQuery(tc.query)
				if r.URL.Path != tc.path || !reflect.DeepEqual(r.URL.Query(), want) {
					t.Errorf("got %s, want %s?%s", r.URL, tc.path, tc.query)
				}
				if tc.kind == "details" {
					fmt.Fprint(w, `{"Id":"movie"}`)
				} else {
					fmt.Fprint(w, `{"Items":[{"Id":"one"}]}`)
				}
			}))
			defer server.Close()
			c := NewClient(Config{Server: server.URL}, Session{UserID: "user"})
			if tc.kind == "details" {
				if _, err := c.Details(context.Background(), "movie"); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := c.List(context.Background(), Location{Kind: tc.kind, SeriesID: "series", ParentID: "season"}, 64, 64); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
