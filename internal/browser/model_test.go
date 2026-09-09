package browser

import (
	"errors"
	"misterfin-go/internal/jellyfin"
	"testing"
)

func TestNavigationCancelsStaleResultsAndRestoresSelection(t *testing.T) {
	m := New()
	req := m.Load(0)
	m.Apply(*req, jellyfin.Page{Items: []jellyfin.Item{{ID: "movies", Name: "Movies", CollectionType: "movies"}, {ID: "music", Name: "Music", CollectionType: "music"}}}, nil)
	m.Key("down")
	load := m.Key("open")
	if load.Location.ParentID != "music" || load.Location.Collection != "music" {
		t.Fatal(load)
	}
	m.Key("back")
	if m.Apply(*load, jellyfin.Page{Items: []jellyfin.Item{{ID: "stale"}}}, nil) {
		t.Fatal("accepted stale response")
	}
	if m.Current().Selected != 1 || m.Current().Title != "Libraries" {
		t.Fatal("lost parent selection")
	}
}

func TestPagingFailurePreservesRowsAndRetriesOffset(t *testing.T) {
	m := New()
	m.Current().Location = jellyfin.Location{Kind: "items", Collection: "movies"}
	total := 503
	items := make([]jellyfin.Item, 64)
	for i := range items {
		items[i].ID = "movie"
	}
	m.Apply(*m.Load(0), jellyfin.Page{Items: items, TotalRecordCount: &total}, nil)
	r := m.Key("next")
	if r.Start != 64 {
		t.Fatal(r)
	}
	m.Apply(*r, jellyfin.Page{}, errors.New("server unavailable"))
	if len(m.Current().Page.Items) != 64 || m.Current().Start != 0 || m.Current().Error == "" {
		t.Fatal("failed page replaced existing rows")
	}
	r = m.Key("retry")
	if r.Start != 64 {
		t.Fatal("retry changed page")
	}
	m.Apply(*r, jellyfin.Page{Items: items, TotalRecordCount: &total}, nil)
	if m.Current().Start != 64 {
		t.Fatal("page did not advance")
	}
	r = m.Key("next")
	m.Apply(*r, jellyfin.Page{Items: []jellyfin.Item{}, TotalRecordCount: &total}, nil)
	if m.Current().Start != 64 || m.Current().Error == "" {
		t.Fatal("empty page hid previous data")
	}
}

func TestSeriesAndUnsupportedItems(t *testing.T) {
	m := New()
	m.Current().Location = jellyfin.Location{Kind: "items", Collection: "mixed"}
	m.Current().Page.Items = []jellyfin.Item{{ID: "series", Type: "Series"}}
	r := m.Key("open")
	if r.Location.Kind != "seasons" || r.Location.SeriesID != "series" {
		t.Fatal(r)
	}
	m.Apply(*r, jellyfin.Page{Items: []jellyfin.Item{{ID: "season", Type: "Season"}}}, nil)
	r = m.Key("open")
	if r.Location.Kind != "episodes" || r.Location.SeriesID != "series" || r.Location.ParentID != "season" {
		t.Fatal(r)
	}
	m.Apply(*r, jellyfin.Page{Items: []jellyfin.Item{{ID: "book", Type: "Book"}}}, nil)
	if r = m.Key("open"); r != nil || m.Current().Detail == nil {
		t.Fatal("unsupported item did not produce details")
	}
}
