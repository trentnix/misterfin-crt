package browser

import "misterfin-crt/internal/media"

const prefetchRows = 24

// centerSelection keeps the highlight near the middle, except at either end
// of the available rows. Scroll and Selected are relative to the retained window.
func (v *View) centerSelection(rows int) {
	rows = max(1, rows)
	v.Scroll = max(0, v.Selected-rows/2)
	if !v.More() {
		v.Scroll = min(v.Scroll, max(0, len(v.Page.Items)-rows))
	}
}

// retainPage joins adjacent responses without changing absolute item positions.
// Each view retains at most three pages. Published item slices are immutable,
// including when a media worker borrows the parent view.
func (v *View) retainPage(start int, page media.Page, target, rows int) {
	end := start + len(page.Items)
	oldEnd := v.Start + len(v.Page.Items)
	if len(v.Page.Items) > 0 && start <= oldEnd && end >= v.Start {
		first, last := min(start, v.Start), max(end, oldEnd)
		// A shorter refreshed page marks the end of the listing.
		if len(page.Items) < PageSize {
			last = end
		}
		if page.TotalRecordCount != nil {
			last = min(last, *page.TotalRecordCount)
		}
		last = max(first, last)
		items := make([]media.Item, last-first)
		if v.Start < last {
			copy(items[v.Start-first:], v.Page.Items)
		}
		if start < last {
			copy(items[start-first:], page.Items)
		}
		page.Items, start = items, first
	}
	target = min(max(start, target), max(start, start+len(page.Items)-1))
	if len(page.Items) > 3*PageSize && v.Location.Kind != "views" && v.Location.Kind != "seasons" {
		first := max(0, target/PageSize*PageSize-PageSize-start)
		// Copy so the discarded page's metadata can be reclaimed.
		page.Items = append([]media.Item(nil), page.Items[first:min(len(page.Items), first+3*PageSize)]...)
		start += first
	}
	v.Page, v.Start = page, start
	v.Selected = min(max(0, target-start), max(0, len(page.Items)-1))
	v.centerSelection(rows)
}

// Prefetch starts one neighboring page near the loaded edge. A failed background
// request waits for explicit navigation or retry instead of polling the server.
func (m *Model) Prefetch() *Request {
	v := m.Current()
	if v.Detail != nil || v.Loading || v.fetching || v.prefetchFailed || len(v.Page.Items) == 0 || v.Location.Kind == "views" || v.Location.Kind == "seasons" {
		return nil
	}
	start := -1
	if v.direction < 0 && v.Start > 0 && v.Selected < prefetchRows {
		start = max(0, v.Start-PageSize)
	} else if v.More() && len(v.Page.Items)-v.Selected <= prefetchRows {
		start = v.Start + len(v.Page.Items)
	}
	if start < 0 {
		return nil
	}
	m.Generation++
	v.fetching = true
	v.PendingStart = start
	return &Request{m.Generation, v.Location, start}
}
