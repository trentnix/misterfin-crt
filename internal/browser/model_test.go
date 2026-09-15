package browser

import (
	"errors"
	"testing"

	"misterfin-crt/internal/input/control"
	"misterfin-crt/internal/jellyfin"
)

func TestNavigationCancelsStaleResultsAndRestoresSelection(t *testing.T) {
	m := New()
	req := m.Load(0)
	m.Apply(*req, jellyfin.Page{Items: []jellyfin.Item{{ID: "movies", Name: "Movies", CollectionType: "movies"}, {ID: "music", Name: "Music", CollectionType: "music"}}}, nil)
	m.Key(control.Next)
	load := m.Key(control.Open)
	if load.Location.ParentID != "music" || load.Location.Collection != "music" {
		t.Fatal(load)
	}
	m.Key(control.Back)
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
	m.ListMode = true
	total := 503
	items := make([]jellyfin.Item, 64)
	for i := range items {
		items[i].ID = "movie"
	}
	m.Apply(*m.Load(0), jellyfin.Page{Items: items, TotalRecordCount: &total}, nil)
	m.Current().Selected = 63
	r := m.Key(control.Down)
	if r.Start != 64 {
		t.Fatal(r)
	}
	m.Apply(*r, jellyfin.Page{}, errors.New("server unavailable"))
	if len(m.Current().Page.Items) != 64 || m.Current().Start != 0 || m.Current().Error == "" {
		t.Fatal("failed page replaced existing rows")
	}
	r = m.Key(control.Retry)
	if r.Start != 64 {
		t.Fatal("retry changed page")
	}
	m.Apply(*r, jellyfin.Page{Items: items, TotalRecordCount: &total}, nil)
	if m.Current().Start != 0 || m.Current().Selected != 64 || len(m.Current().Page.Items) != 128 {
		t.Fatal("page did not advance")
	}
	m.Current().Selected = 127
	r = m.Key(control.Down)
	m.Apply(*r, jellyfin.Page{Items: []jellyfin.Item{}, TotalRecordCount: &total}, nil)
	if m.Current().Start != 0 || len(m.Current().Page.Items) != 128 || m.Current().Error == "" {
		t.Fatal("empty page hid previous data")
	}
}

func TestSeriesAndUnsupportedItems(t *testing.T) {
	m := New()
	m.Current().Location = jellyfin.Location{Kind: "items", Collection: "mixed"}
	m.Current().Page.Items = []jellyfin.Item{{ID: "series", Type: "Series"}}
	r := m.Key(control.Open)
	if r.Location.Kind != "seasons" || r.Location.SeriesID != "series" {
		t.Fatal(r)
	}
	m.Apply(*r, jellyfin.Page{Items: []jellyfin.Item{{ID: "season", Type: "Season"}}}, nil)
	r = m.Key(control.Open)
	if r.Location.Kind != "episodes" || r.Location.SeriesID != "series" || r.Location.ParentID != "season" {
		t.Fatal(r)
	}
	m.Apply(*r, jellyfin.Page{Items: []jellyfin.Item{{ID: "book", Type: "Book"}}}, nil)
	if r = m.Key(control.Open); r != nil || m.Current().Detail == nil {
		t.Fatal("unsupported item did not produce details")
	}
}

func TestCarouselListToggleAndExit(t *testing.T) {
	m := New()
	m.Current().Page.Items = make([]jellyfin.Item, 4)
	m.Key(control.Down)
	if m.Current().Selected != 0 {
		t.Fatal("carousel moved vertically")
	}
	m.Key(control.Next)
	m.Key(control.Select)
	m.Key(control.Down)
	if !m.ListMode || m.Current().Selected != 2 {
		t.Fatal("list toggle lost selection")
	}
	m.Key(control.Back)
	if !m.ExitConfirm || m.Quit {
		t.Fatal("root back must confirm")
	}
	m.Key(control.Back)
	if m.ExitConfirm {
		t.Fatal("cancel did not close dialog")
	}
	m.Key(control.Back)
	m.Key(control.Open)
	if !m.Quit {
		t.Fatal("confirm did not quit")
	}
}
func TestScreenJumpAndBackwardPageBoundary(t *testing.T) {
	m := New()
	m.Rows = 6
	v := m.Current()
	v.Location.Kind = "items"
	m.ListMode = true
	total := 130
	items := make([]jellyfin.Item, 64)
	m.Apply(*m.Load(0), jellyfin.Page{Items: items, TotalRecordCount: &total}, nil)
	if m.Key(control.Next) != nil || v.Selected != 6 || v.Scroll != 3 {
		t.Fatal("jump must move one screen")
	}
	v.Selected = 63
	r := m.Key(control.Down)
	m.Apply(*r, jellyfin.Page{Items: items, TotalRecordCount: &total}, nil)
	r = m.Key(control.Up)
	if r != nil {
		t.Fatal("cached previous page caused a request")
	}
	if v.Selected != 63 || v.Scroll != 60 {
		t.Fatalf("backward crossing: %+v", v)
	}
}
