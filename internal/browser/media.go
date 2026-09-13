package browser

import (
	"context"
	"misterfin-crt/internal/jellyfin"
	"misterfin-crt/internal/playback"
)

func resumableVideo(item *jellyfin.Item) bool {
	return item != nil && playback.Supported(*item) && item.Type != "Audio" &&
		!jellyfin.IsLive(*item) && !item.UserData.Played && item.UserData.PlaybackPositionTicks > 0
}

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
			parent.retainPage(start, page, index, rows)
			if index >= parent.Start+len(parent.Page.Items) {
				return parent, nil, nil
			}
		}
		item := parent.Page.Items[index-parent.Start]
		if item.Type == kind {
			parent.Selected, parent.Target = index-parent.Start, index
			parent.centerSelection(rows)
			return parent, &item, nil
		}
		if kind == "Audio" {
			return parent, nil, nil
		}
		index += direction
	}
	return parent, nil, nil
}
