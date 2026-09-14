package playback

import "misterfin-crt/internal/player"

// PictureMode controls recorded-video fit before shared UI composition.
type PictureMode = player.PictureMode

const (
	// PictureOriginal preserves the full frame and its display aspect ratio.
	PictureOriginal = player.PictureOriginal
	// PictureZoom43 enlarges and crops the center, including native 4:3 sources.
	PictureZoom43 = player.PictureZoom43
)

// PictureResult acknowledges one live request. Err leaves the preceding mode active.
type PictureResult struct {
	Request int
	Mode    PictureMode
	Err     error
}
