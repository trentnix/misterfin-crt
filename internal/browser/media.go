package browser

import (
	"context"

	"misterfin-crt/internal/media"
	"misterfin-crt/internal/playback"
)

func resumableVideo(item *media.Item) bool {
	return item != nil && playback.Supported(*item) && item.Type != "Audio" &&
		!media.IsLive(*item) && !item.UserData.Played && item.UserData.PlaybackPositionTicks > 0
}

// Find adjacent media without replacing the visible page until a match arrives.
// Photos skip other item types. Playlists advance through playable audio/video
// entries in server order. Ordinary music folders end at a non-audio item.
func adjacentMedia(ctx context.Context, c itemLister, parent View, kind string, direction, rows int) (View, *media.Item, error) {
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
		if item.Type == kind || (kind == "playlist" && playback.Supported(item) && !media.IsLive(item)) {
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

// itemLister pages through siblings without requiring playback or artwork access.
type itemLister interface {
	List(context.Context, media.Location, int, int) (media.Page, error)
}
