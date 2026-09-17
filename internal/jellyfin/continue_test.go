package jellyfin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func continuing(id, series string, position int64, day int) Item {
	item := Item{ID: id, Name: id, Type: "Movie", SeriesID: series}
	if series != "" {
		item.Type = "Episode"
	}
	item.UserData.PlaybackPositionTicks = position
	if day > 0 {
		date := time.Date(2026, 9, day, 0, 0, 0, 0, time.UTC)
		item.UserData.LastPlayedDate = &date
	}
	return item
}

func TestContinueWatchingQueriesAndPagination(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("userId") != "user" || q.Get("EnableUserData") != "true" || q.Get("Fields") != "ProductionYear,RunTimeTicks,SeriesInfo" {
			t.Errorf("wrong home query: %v", q)
		}
		switch r.URL.Path {
		case "/UserItems/Resume":
			if q.Get("MediaTypes") != "Video" {
				t.Error("resume includes non-video")
			}
			start, _ := strconv.Atoi(q.Get("StartIndex"))
			items := []Item{}
			for i := start; i < min(start+128, 130); i++ {
				items = append(items, continuing(fmt.Sprint("movie-", i), "", 100, 1))
			}
			total := 130
			json.NewEncoder(w).Encode(Page{Items: items, TotalRecordCount: &total})
		case "/Shows/NextUp":
			if q.Get("enableResumable") != "false" {
				t.Error("next up included resumable episodes")
			}
			next := continuing("next", "series", 0, 0)
			next.Type = "" // Jellyfin's NextUp response may omit the item type.
			json.NewEncoder(w).Encode(Page{Items: []Item{next}})
		case "/Items":
			if q.Get("SortBy") != "DatePlayed" || q.Get("SortOrder") != "Descending" || q.Get("IsPlayed") != "true" || q.Get("Limit") != "256" {
				t.Error("wrong activity query", q)
			}
			json.NewEncoder(w).Encode(Page{Items: []Item{continuing("done", "series", 0, 12)}})
		default:
			t.Error("unexpected request", r.URL.Path)
		}
	}))
	defer server.Close()
	c := NewClient(Config{Server: server.URL}, Session{UserID: "user"})
	page, err := c.ContinueWatching(context.Background())
	if err != nil || len(page.Items) != 131 || *page.TotalRecordCount != 131 || page.Items[0].ID != "next" || page.Items[0].Type != "Episode" {
		t.Fatalf("count %d: %v", len(page.Items), err)
	}
}

func TestContinueWatchingRetainsPartialResultsAndCancels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/UserItems/Resume" {
			json.NewEncoder(w).Encode(Page{Items: []Item{continuing("movie", "", 100, 1)}})
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	c := NewClient(Config{Server: server.URL}, Session{})
	page, err := c.ContinueWatching(context.Background())
	if err == nil || len(page.Items) != 1 {
		t.Fatalf("partial result lost: %+v %v", page, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = c.ContinueWatching(ctx); err != context.Canceled {
		t.Fatal("cancellation lost", err)
	}
}
