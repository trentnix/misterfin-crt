package browser

import (
	"context"
	"misterfin-go/internal/jellyfin"
	"time"
)

// The three-second reveal window follows bb31e83 and src/pause_ui.c.
func (m *Model) RevealControls(now time.Time) bool {
	hidden := !m.ControlsVisible(now)
	m.ControlsUntil = now.Add(3 * time.Second)
	return hidden
}
func (m *Model) ControlsVisible(now time.Time) bool { return now.Before(m.ControlsUntil) }
func (m *Model) HideControls()                      { m.ControlsUntil = time.Time{} }

// Find adjacent media without replacing the visible page until a match arrives.
// Photos skip other item types. A music queue ends at a non-audio item, as in C.
func adjacentMedia(ctx context.Context, c *jellyfin.Client, parent View, kind string, direction, rows int) (View, *jellyfin.Item, error) {
	if direction != 1 && direction != -1 {
		return parent, nil, nil
	}
	index := parent.Start + parent.Selected + direction
	for index >= 0 {
		if err := ctx.Err(); err != nil {
			return parent, nil, err
		}
		if parent.Page.TotalRecordCount != nil && index >= *parent.Page.TotalRecordCount {
			return parent, nil, nil
		}
		if index < parent.Start || index >= parent.Start+len(parent.Page.Items) {
			if index >= parent.Start+len(parent.Page.Items) && !parent.More() {
				return parent, nil, nil
			}
			start := index / PageSize * PageSize
			page, err := c.List(ctx, parent.Location, start, PageSize)
			if err != nil {
				return parent, nil, err
			}
			if len(page.Items) == 0 {
				return parent, nil, nil
			}
			parent.Page, parent.Start = page, start
			if index >= start+len(page.Items) {
				return parent, nil, nil
			}
		}
		item := parent.Page.Items[index-parent.Start]
		if item.Type == kind {
			parent.Selected, parent.Target = index-parent.Start, index
			parent.Scroll = max(0, parent.Selected-max(1, rows)+1)
			return parent, &item, nil
		}
		if kind == "Audio" {
			return parent, nil, nil
		}
		index += direction
	}
	return parent, nil, nil
}
