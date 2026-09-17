package rendering

import (
	"image"
	"time"

	"mistervision/internal/input/control"
	"mistervision/internal/musicviz"
)

// Scene is the input to a renderer, separate from the mutable navigation model.
// Scalar state is copied. Content items, artwork, and control labels are borrowed
// read-only during Render. Renderers may retain immutable artwork for caching,
// but must not retain or mutate Content slices or detail pointers after Render returns.
type Scene struct {
	// Title borrows the immutable root heading. Nil uses MiSTerVision.
	// An empty value hides the heading.
	Title *string

	// Background is borrowed immutable artwork for carousel and list screens.
	// Nil selects the normal per-library and per-item backgrounds.
	Background image.Image

	Message        MessagePresentation
	About          AboutPresentation
	Content        Content
	Root           bool
	ListMode       bool
	ExitConfirm    bool
	Notice         string
	Setup          SetupPresentation
	SelectionError string
	PhotoCount     string
	Artwork        Artwork
	LibraryCount   *int
	LibraryLoading bool // The selected home card is waiting for its initial feed.

	// Controls borrows immutable labels from the last active input device.
	Controls control.Labels

	// Music contains immutable preset data. MusicFrame is a copied audio snapshot.
	Music                *musicviz.Library
	MusicIndex           int
	MusicFrame           musicviz.Frame
	MusicLabel           bool
	MusicMessage         string
	Shuffle              bool
	Audio                bool
	Video                bool
	PhotoControlsVisible bool

	// Playback supplies decoder controls and timing. Photo controls belong to navigation.
	Playback PlaybackPresentation
	Now      time.Time
}

func (s Scene) title() string {
	if s.Audio && s.Content.Detail != nil {
		return "Now playing"
	}
	if s.Root {
		if s.Title != nil {
			return *s.Title
		}
		return "MiSTerVision"
	}
	return s.Content.Title
}
