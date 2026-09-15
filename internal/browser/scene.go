package browser

import (
	"image"
	"time"

	"misterfin-crt/internal/input/control"
	"misterfin-crt/internal/musicviz"
)

// Scene is the input to a renderer, separate from the mutable navigation model.
// Scalar state is copied. View items, artwork, and control labels are borrowed
// read-only during Render. Renderers may retain immutable artwork for caching,
// but must not retain or mutate View slices or detail pointers after Render returns.
type Scene struct {
	// Title borrows the immutable root heading. Nil uses MiSTerFin CRT.
	// An empty value hides the heading.
	Title *string

	// Background is borrowed immutable artwork for carousel and list screens.
	// Nil selects the normal per-library and per-item backgrounds.
	Background image.Image

	Message        MessagePresentation
	About          AboutPresentation
	View           View
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

// sceneFromModel combines navigation and a decoder snapshot once per frame.
// Music stays visible between tracks. Photo menus never read decoder state.
func sceneFromModel(m *Model, playback PlaybackPresentation, setup SetupPresentation, selection selectionData, selectionError string, now time.Time) Scene {
	s := Scene{
		View:                 *m.Current(),
		Root:                 len(m.Stack) == 1,
		ListMode:             m.ListMode,
		ExitConfirm:          m.ExitConfirm,
		Notice:               m.Notice,
		Setup:                setup,
		Artwork:              selection.artwork,
		LibraryCount:         selection.count,
		SelectionError:       selectionError,
		Audio:                m.MusicQueueActive(),
		Video:                playback.Active && !playback.Audio,
		Playback:             playback,
		Now:                  now,
		PhotoControlsVisible: m.PhotoControlsVisible(now),
	}
	if parent, ok := m.Parent(); ok {
		s.PhotoCount = parent.Count()
	}
	return s
}

func (s Scene) title() string {
	if s.Audio && s.View.Detail != nil {
		return "Now playing"
	}
	if s.Root {
		if s.Title != nil {
			return *s.Title
		}
		return "MiSTerFin CRT"
	}
	return s.View.Title
}
