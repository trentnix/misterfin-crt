package browser

import (
	"context"

	"misterfin-crt/internal/jellyfin"
)

// selectionState owns the selected metadata, images, and request lifetime.
type selectionState struct {
	cancel     context.CancelFunc
	generation int
	key, err   string
	current    selectionData
	loader     *selectionLoader
}

// loadSelection follows the selected item and view type. An unchanged selection
// reuses current work. A new selection publishes cached metadata and images immediately
// and rejects later updates from the previous selection by generation.
func (s *browserSession) loadSelection() {
	item := s.model.Current().Item()
	key := ""
	root := len(s.model.Stack) == 1
	detail := s.model.Current().Detail != nil
	if item != nil {
		key = item.ID
		if root {
			key = "root:" + key
		}
		if detail {
			key = "detail:" + key
		}
	}
	if key == s.selection.key {
		return
	}
	s.selection.key = key
	s.selection.cancel()
	s.selection.generation++
	s.selection.current = selectionData{}
	s.selection.err = ""
	if item == nil || s.client == nil {
		return
	}
	if item.ID == continueID {
		s.seedHomeArtwork()
	}
	s.selection.current = s.selection.loader.snapshot(*item, root)
	if item.ID == continueID && s.home.err != nil {
		s.selection.err = "Continue Watching incomplete."
	}
	selected := *item
	generation := s.selection.generation
	work, stop := context.WithCancel(s.ctx)
	s.selection.cancel = stop
	loader := s.selection.loader
	go loader.load(work, selected, root, detail, func(update selectionUpdate) {
		s.send(work, result{kind: selectionResult, selectionGeneration: generation, update: update})
	})
}

func (s *browserSession) handleSelection(r result) bool {
	if r.selectionGeneration != s.selection.generation {
		return false
	}
	if jellyfin.Rejected(r.update.err) {
		s.status = "Session rejected. Press R to sign in again."
		return false
	}
	if r.update.err != nil {
		switch r.update.kind {
		case selectionDetails:
			s.selection.err = "Details unavailable."
		case selectionCount:
			s.selection.err = "Library count unavailable."
		default:
			s.selection.err = "Artwork unavailable."
		}
	} else {
		switch r.update.kind {
		case selectionArtwork:
			applyArtwork(&s.selection.current.artwork, r.update.art)
		case selectionCount:
			s.selection.current.count = r.update.count
		case selectionDetails:
			if s.model.Current().Detail != nil {
				s.model.Current().Detail = r.update.detail
			}
		}
	}

	return true
}
