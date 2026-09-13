package browser

import (
	"context"

	"misterfin-go/internal/jellyfin"
)

// artworkState keeps selection identity, cached images, and request lifetime together.
type artworkState struct {
	cancel     context.CancelFunc
	generation int
	key, err   string
	current    Artwork
	loader     *artworkLoader
}

// loadArt follows the selected item and view type. An unchanged selection
// reuses the current work. A new selection publishes cached artwork immediately
// and rejects later updates from the previous selection by generation.
func (s *browserSession) loadArt() {
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
	if key == s.artwork.key {
		return
	}
	s.artwork.key = key
	s.artwork.cancel()
	s.artwork.generation++
	s.artwork.current = Artwork{}
	s.artwork.err = ""
	if item == nil || s.client == nil {
		return
	}
	s.artwork.current = s.artwork.loader.cache.snapshot(*item, root)
	selected := *item
	generation := s.artwork.generation
	work, stop := context.WithCancel(s.ctx)
	s.artwork.cancel = stop
	loader := s.artwork.loader
	go loader.load(work, selected, root, detail, func(update artUpdate) {
		s.send(work, result{kind: artworkResult, imageID: generation, update: update})
	})
}

func (s *browserSession) handleArtwork(r result) bool {
	if r.imageID != s.artwork.generation {
		return false
	}
	if jellyfin.Rejected(r.update.err) {
		s.status = "Session rejected. Press R to sign in again."
		return false
	}
	if r.update.err != nil {
		switch r.update.kind {
		case "detail":
			s.artwork.err = "Details unavailable. R:retry"
		case "count":
			s.artwork.err = "Library count unavailable. R:retry"
		default:
			s.artwork.err = "Artwork unavailable. R:retry"
		}
	} else {
		applyArtwork(&s.artwork.current, r.update)
		if r.update.kind == "detail" && s.model.Current().Detail != nil {
			s.model.Current().Detail = r.update.detail
		}
	}

	return true
}
