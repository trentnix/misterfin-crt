package rendering

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"mistervision/internal/jellyfin"
	"mistervision/internal/ui"
)

func TestListAnimationPreservesPositionWhenWindowMoves(t *testing.T) {
	m := Scene{ListMode: true, Content: Content{Start: 0, Selected: 127, Scroll: 124}}
	s := testScene(m, PlaybackPresentation{}, SetupPresentation{}, Artwork{}, "", time.Unix(0, 0))
	var a animationState
	a.advance(s, 6)
	// Dropping an old page changes relative indices, not the visible position.
	s.Content.Start, s.Content.Selected, s.Content.Scroll = 64, 63, 60
	s.Now = s.Now.Add(time.Millisecond * 16)
	if got := a.advance(s, 6); got.ScrollOffset != 0 {
		t.Fatal("page rebase animated an artificial jump")
	}
	s.Content.Scroll++
	s.Now = s.Now.Add(time.Millisecond * 16)
	if got := a.advance(s, 6); got.ScrollOffset >= 0 || got.ScrollOffset <= -1 {
		t.Fatal("rows did not ease between positions")
	}
}

// Page retention must not change a frame, including during fractional scrolling.
func TestListWindowRebasePreservesPixelsAndClipsRows(t *testing.T) {
	for _, height := range []int{240, 288} {

		rows := VisibleRows(640, height)
		total := 400
		m := Scene{Root: true, ListMode: true, Content: Content{Selected: 100, Scroll: 100 - rows/2, Page: jellyfin.Page{TotalRecordCount: &total}}}
		for i := 0; i < 192; i++ {
			m.Content.Page.Items = append(m.Content.Page.Items, jellyfin.Item{ID: fmt.Sprint(i), Name: fmt.Sprint(i)})
		}
		s := testScene(m, PlaybackPresentation{}, SetupPresentation{}, Artwork{}, "", time.Unix(0, 0))
		anim := Animation{Row: float64(rows / 2), ScrollOffset: -0.4}
		want := renderScene(ui.New(640, height), nil, s, anim)
		s.Content.Page.Items = s.Content.Page.Items[64:]
		s.Content.Start += 64
		s.Content.Selected -= 64
		s.Content.Scroll -= 64
		got := renderScene(ui.New(640, height), nil, s, anim)
		if !bytes.Equal(got, want) {
			t.Fatal("discarding an old page changed visible pixels")
		}
		anim.ScrollOffset = 0
		still := renderScene(ui.New(640, height), nil, s, anim)
		top := (safeY(640, height) + 21) * 640 * 4
		bottom := top + rows*30*640*4
		if !bytes.Equal(got[:top], still[:top]) || !bytes.Equal(got[bottom:], still[bottom:]) {
			t.Fatal("moving rows overwrote header or footer")
		}
	}
}
