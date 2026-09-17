package browser

import (
	"time"

	"mistervision/internal/rendering"
)

// sceneFromModel combines navigation and a decoder snapshot once per frame.
// Music stays visible between tracks. Photo menus never read decoder state.
func sceneFromModel(m *Model, playback rendering.PlaybackPresentation, setup rendering.SetupPresentation, selection selectionData, selectionError string, now time.Time) rendering.Scene {
	s := rendering.Scene{
		Content:              contentFromView(m.Current()),
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
		s.PhotoCount = contentFromView(&parent).Count()
	}
	return s
}

// contentFromView borrows the visible items and copies only presentation state.
// Request targets, generations, and retained navigation history stay in browser.
func contentFromView(v *View) rendering.Content {
	return rendering.Content{
		Identity:   [4]string{v.Location.Kind, v.Location.ParentID, v.Location.Collection, v.Location.SeriesID},
		Title:      v.Title,
		Page:       v.Page,
		Start:      v.Start,
		Selected:   v.Selected,
		Scroll:     v.Scroll,
		Detail:     v.Detail,
		Loading:    v.Loading,
		Fetching:   v.fetching,
		Error:      v.Error,
		Continue:   v.Location.Kind == "continue",
		CanShuffle: canShuffle(*v),
		CanResume:  resumableVideo(v.Detail),
	}
}
