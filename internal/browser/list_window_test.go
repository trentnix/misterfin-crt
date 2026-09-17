package browser

import (
	"errors"
	"fmt"
	"testing"

	"mistervision/internal/input/control"
	"mistervision/internal/jellyfin"
)

func windowPage(start, total int) jellyfin.Page {
	items := make([]jellyfin.Item, min(PageSize, max(0, total-start)))
	for i := range items {
		items[i] = jellyfin.Item{ID: fmt.Sprint(start + i), Name: fmt.Sprint(start + i)}
	}
	return jellyfin.Page{Items: items, TotalRecordCount: &total}
}

func windowModel(total, rows int) *Model {
	m := New()
	m.ListMode, m.Rows = true, rows
	m.Current().Location = jellyfin.Location{Kind: "items", Collection: "music"}
	m.Apply(*m.Load(0), windowPage(0, total), nil)
	return m
}

func TestContinuousListAcrossPagesAndEnds(t *testing.T) {
	for _, rows := range []int{6, 7} {
		t.Run(fmt.Sprint(rows), func(t *testing.T) {
			const total = 330
			m := windowModel(total, rows)
			v := m.Current()
			check := func(index int) {
				t.Helper()
				if v.Item().ID != fmt.Sprint(index) || v.Start+v.Selected != index {
					t.Fatalf("selection moved at %d: start=%d selected=%d", index, v.Start, v.Selected)
				}
				wantScroll := min(max(0, index-rows/2), total-rows)
				if v.Start+v.Scroll != wantScroll {
					t.Fatalf("at %d: scroll=%d want=%d", index, v.Start+v.Scroll, wantScroll)
				}
				if len(v.Page.Items) > 3*PageSize {
					t.Fatal("unbounded metadata retention")
				}
			}
			move := func(key control.Action, index int) {
				t.Helper()
				if r := m.Key(key); r != nil {
					t.Fatalf("prefetched boundary blocked at %d", index)
				}
				if r := m.Prefetch(); r != nil {
					if v.Loading {
						t.Fatal("prefetch displayed a loading message")
					}
					m.Apply(*r, windowPage(r.Start, total), nil)
				}
				check(index)
			}
			check(0)
			for i := 1; i < total; i++ {
				move("down", i)
			}
			move("down", total-1)
			for i := total - 2; i >= 0; i-- {
				move("up", i)
			}
			move("up", 0)
		})
	}
}

func TestSlowPrefetchPreservesRowsAndCanReverse(t *testing.T) {
	m := windowModel(200, 6)
	v := m.Current()
	for i := 0; i < 40; i++ {
		m.Key(control.Down)
	}
	r := m.Prefetch()
	if r == nil || r.Start != 64 {
		t.Fatal("missing early prefetch")
	}
	for i := 40; i < 64; i++ {
		if next := m.Key(control.Down); next != nil {
			t.Fatal("duplicated pending request")
		}
	}
	if !v.Loading || v.Item().ID != "63" || len(v.Page.Items) != 64 || v.Selected-v.Scroll != 3 {
		t.Fatal("slow page cleared current rows")
	}
	m.Key(control.Up)
	if v.Loading || v.Item().ID != "62" {
		t.Fatal("cannot reverse while page is pending")
	}
	m.Apply(*r, windowPage(64, 200), nil)
	if v.Item().ID != "62" || v.Selected-v.Scroll != 3 {
		t.Fatal("late prefetch changed selection")
	}
}

func TestPrefetchFailureAndCancellation(t *testing.T) {
	m := windowModel(200, 6)
	v := m.Current()
	v.Selected = 63
	r := m.Prefetch()
	m.Apply(*r, jellyfin.Page{}, errors.New("offline"))
	if v.Error != "" || m.Prefetch() != nil {
		t.Fatal("background failure interrupted browsing or retried in a loop")
	}
	r = m.Key(control.Down)
	m.Apply(*r, jellyfin.Page{}, errors.New("offline"))
	if !v.prefetchFailed || v.Error == "" || v.Item().ID != "63" {
		t.Fatal("foreground failure lost rows or error")
	}
	r = m.Key(control.Retry)
	m.Apply(*r, windowPage(64, 200), nil)
	if v.Item().ID != "64" {
		t.Fatal("retry lost target")
	}
	v.Selected = 110
	r = m.Prefetch()
	if r == nil {
		t.Fatal("missing prefetch")
	}
	m.Key(control.Open)
	if m.Apply(*r, windowPage(r.Start, 200), nil) {
		t.Fatal("prefetch replaced another screen")
	}
}

func TestUnknownTotalFindsEndWithoutDiscardingRows(t *testing.T) {
	m := windowModel(64, 6)
	v := m.Current()
	v.Page.TotalRecordCount = nil
	v.Selected = 63
	r := m.Prefetch()
	m.Key(control.Down)
	m.Apply(*r, jellyfin.Page{}, nil)
	if v.More() || v.Loading || v.Error != "" || v.Item().ID != "63" {
		t.Fatal("empty final page lost rows or kept requesting")
	}
}
