package browser

import "time"

// Scene is the input to a renderer, separate from the mutable navigation model.
// Scalar state is copied. View items and artwork are borrowed read-only during
// Render. Renderers may retain immutable artwork for caching, but must not retain
// or mutate View slices or detail pointers after Render returns.
type Scene struct {
	View         View
	Root         bool
	ListMode     bool
	ExitConfirm  bool
	Notice       string
	Status       string
	ArtworkError string
	PhotoCount   string
	Artwork      Artwork

	Audio bool
	Video bool

	// One playback snapshot supplies controls and timing for every media screen.
	Playback PlaybackPresentation
	Now      time.Time
}

func sceneFromModel(m *Model, status string, art Artwork, artError string, now time.Time) Scene {
	s := Scene{
		View:         *m.Current(),
		Root:         len(m.Stack) == 1,
		ListMode:     m.ListMode,
		ExitConfirm:  m.ExitConfirm,
		Notice:       m.Notice,
		Status:       status,
		Artwork:      art,
		ArtworkError: artError,
		Audio:        m.PlayingAudio,
		Video:        m.PlayingVideo,
		Playback:     m.PlaybackState.presentation(m.Current().Detail, now),
		Now:          now,
	}
	if len(m.Stack) > 1 {
		s.PhotoCount = m.Stack[len(m.Stack)-2].Count()
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
