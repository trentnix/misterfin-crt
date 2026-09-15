package jellyfin

import "slices"

// QueueItem identifies a media occurrence in Jellyfin's remote playback queue.
type QueueItem struct {
	ID             string `json:"Id"`
	PlaylistItemID string `json:"PlaylistItemId"`
}

// PlaybackQueue supplies queue metadata to the existing progress reporter.
// ItemID scopes the snapshot so an old decoder cannot report a newer item's queue.
type PlaybackQueue struct {
	ItemID                             string
	Items                              []QueueItem
	Current, RepeatMode, PlaybackOrder string
}

// SetPlaybackQueue publishes an owned snapshot without blocking decoder reports.
func (c *Client) SetPlaybackQueue(q PlaybackQueue) {
	q.Items = slices.Clone(q.Items)
	c.queue.Store(&q)
}
