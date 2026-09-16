package rendering

import (
	"fmt"

	"misterfin-crt/internal/media"
)

// Content describes the visible items and selection without navigation or request
// state. Page.Items and Detail are borrowed read-only for one Render call.
// Identity is an opaque comparable key. A change resets list scroll animation.
type Content struct {
	// Identity distinguishes lists without exposing a browser request. Its parts
	// preserve equality without allocating a combined key on every frame.
	Identity [4]string
	Title    string
	// Page borrows immutable items and their optional total. Start is the absolute
	// index of its first item. Selected and Scroll are relative to that window.
	Page                    media.Page
	Start, Selected, Scroll int
	Detail                  *media.Item
	Loading, Fetching       bool
	Error                   string
	// Continue selects resume/next-episode labels. Capabilities describe actions
	// the caller supports. Drawing must not infer navigation or playback policy.
	Continue, CanShuffle, CanResume bool
}

// Item returns the borrowed detail or selected item, or nil for an empty list.
func (c Content) Item() *media.Item {
	if c.Detail != nil {
		return c.Detail
	}
	if c.Selected < 0 || c.Selected >= len(c.Page.Items) {
		return nil
	}
	return &c.Page.Items[c.Selected]
}

// Count describes the selection's absolute position. An unknown total uses "?".
func (c Content) Count() string {
	if len(c.Page.Items) == 0 {
		return "0 items"
	}
	total := "?"
	if c.Page.TotalRecordCount != nil {
		total = fmt.Sprint(*c.Page.TotalRecordCount)
	}
	return fmt.Sprintf("%d/%s", c.Start+c.Selected+1, total)
}
