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

func TestMergeContinuePrefersResumeAndOrdersSeriesByActivity(t *testing.T) {
	resume := []Item{continuing("movie", "", 100, 8), continuing("old-episode", "series", 100, 5), continuing("episode", "series", 120, 10)}
	next := []Item{continuing("series-next", "series", 0, 0), continuing("other-next", "other", 0, 0), continuing("unknown-next", "unknown", 0, 0), continuing("unknown-next", "unknown", 0, 0)}
	history := []Item{continuing("finished", "other", 0, 12)}
	got := mergeContinue(resume, next, history)
	want := []string{"other-next", "episode", "movie", "unknown-next"}
	if len(got) != len(want) {
		t.Fatal(got)
	}
	for i, id := range want {
		if got[i].ID != id {
			t.Errorf("position %d: %s, want %s", i, got[i].ID, id)
		}
	}
	if got[0].ContinueAction != "next" || got[1].ContinueAction != "resume" {
		t.Fatal("missing action labels")
	}
	if resume[0].ContinueAction != "" || resume[0].ID != "movie" {
		t.Fatal("merge mutated source data")
	}
	played := continuing("played", "", 100, 12)
	played.UserData.Played = true
	if got := mergeContinue([]Item{played, continuing("zero", "", 0, 0)}, []Item{played, continuing("resumable-next", "x", 100, 0)}, nil); len(got) != 0 {
		t.Fatal("finished or resumable next items retained", got)
	}
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
			json.NewEncoder(w).Encode(Page{Items: []Item{continuing("next", "series", 0, 0)}})
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
	if err != nil || len(page.Items) != 131 || *page.TotalRecordCount != 131 || page.Items[0].ID != "next" {
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
