package media

import (
	"reflect"
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
	got := MergeContinueWatching(resume, next, history)
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
	if got := MergeContinueWatching([]Item{played, continuing("zero", "", 0, 0)}, []Item{played, continuing("resumable-next", "x", 100, 0)}, nil); len(got) != 0 {
		t.Fatal("finished or resumable next items retained", got)
	}
}

// Equal and missing dates retain provider ordering, while returned presentation
// metadata must not change the adapter's original items.
func TestMergeContinueWatchingStableOrderAndOwnership(t *testing.T) {
	resume := []Item{continuing("movie-b", "", 100, 5), continuing("movie-a", "", 100, 5)}
	next := []Item{continuing("series-b", "b", 0, 0), continuing("series-a", "a", 0, 0), continuing("series-b", "b", 0, 0)}
	beforeResume := append([]Item(nil), resume...)
	beforeNext := append([]Item(nil), next...)
	got := MergeContinueWatching(resume, next, nil)
	want := []string{"movie-b", "movie-a", "series-b", "series-a"}
	if len(got) != len(want) {
		t.Fatal("unexpected count", len(got))
	}
	for i, id := range want {
		if got[i].ID != id {
			t.Fatalf("item %d = %s, want %s", i, got[i].ID, id)
		}
	}
	if !reflect.DeepEqual(resume, beforeResume) || !reflect.DeepEqual(next, beforeNext) {
		t.Fatal("modified input items")
	}
	got[0].Name = "changed"
	if resume[0].Name != beforeResume[0].Name {
		t.Fatal("result shares input slice")
	}
	if got := MergeContinueWatching(nil, nil, nil); len(got) != 0 {
		t.Fatal("empty inputs produced items")
	}
}
