package jellyfin

import "misterfin-crt/internal/media"

// Wire-compatible aliases keep Jellyfin JSON decoding on the shared media model.
type (
	MediaStream = media.MediaStream
	MediaSource = media.MediaSource
	Item        = media.Item
	Page        = media.Page
	Location    = media.Location
)
