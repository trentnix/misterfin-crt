package browser

import "time"

// Scene is the input to a renderer, separate from the mutable navigation model.
// Scalar state is copied. View items and artwork are borrowed read-only during
// Render. Renderers may retain immutable artwork for caching, but must not retain
// or mutate View slices or detail pointers after Render returns.
type Scene struct {
	View           View
	Root           bool
	ListMode       bool
	ExitConfirm    bool
	Notice         string
	Status         string
	SelectionError string
	PhotoCount     string
	Artwork        Artwork
	LibraryCount   *int

	Audio                bool
	Video                bool
	PhotoControlsVisible bool

	// Playback supplies decoder controls and timing. Photo controls belong to navigation.
	Playback PlaybackPresentation
	Now      time.Time
}

// sceneFromModel combines navigation and a decoder snapshot once per frame.
// Music stays visible between tracks. Photo menus never read decoder state.
func sceneFromModel(m *Model, playback PlaybackPresentation, status string, selection selectionData, selectionError string, now time.Time) Scene {
	s := Scene{
		View:                 *m.Current(),
		Root:                 len(m.Stack) == 1,
		ListMode:             m.ListMode,
		ExitConfirm:          m.ExitConfirm,
		Notice:               m.Notice,
		Status:               status,
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
		return "MiSTerFin-Go"
	}
	return s.View.Title
}
