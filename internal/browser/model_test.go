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
	m.Key("next")
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
	m.ListMode = true
	total := 503
	items := make([]jellyfin.Item, 64)
	for i := range items {
		items[i].ID = "movie"
	}
	m.Apply(*m.Load(0), jellyfin.Page{Items: items, TotalRecordCount: &total}, nil)
	m.Current().Selected = 63
	r := m.Key("down")
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
	m.Current().Selected = 63
	r = m.Key("down")
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

func TestCarouselListToggleAndExit(t *testing.T) {
	m := New()
	m.Current().Page.Items = make([]jellyfin.Item, 4)
	m.Key("down")
	if m.Current().Selected != 0 {
		t.Fatal("carousel moved vertically")
	}
	m.Key("next")
	m.Key("select")
	m.Key("down")
	if !m.ListMode || m.Current().Selected != 2 {
		t.Fatal("list toggle lost selection")
	}
	m.Key("back")
	if !m.ExitConfirm || m.Quit {
		t.Fatal("root back must confirm")
	}
	m.Key("back")
	if m.ExitConfirm {
		t.Fatal("cancel did not close dialog")
	}
	m.Key("back")
	m.Key("open")
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
	if m.Key("next") != nil || v.Selected != 6 || v.Scroll != 1 {
		t.Fatal("jump must move one screen")
	}
	v.Selected = 63
	r := m.Key("down")
	m.Apply(*r, jellyfin.Page{Items: items, TotalRecordCount: &total}, nil)
	r = m.Key("up")
	if r == nil || r.Start != 0 {
		t.Fatal("missing previous page")
	}
	m.Apply(*r, jellyfin.Page{Items: items, TotalRecordCount: &total}, nil)
	if v.Selected != 63 || v.Scroll != 58 {
		t.Fatalf("backward crossing: %+v", v)
	}
}
