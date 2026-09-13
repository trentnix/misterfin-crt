package browser

import "misterfin-go/internal/jellyfin"

// resultKind identifies the worker that produced a result. Request generation
// checks remain with each handler, beside the state they protect.
type resultKind uint8

const (
	pageResult resultKind = iota
	authResult
	artworkResult
	neighborResult
)

// result carries one worker result, selected by kind. Page and authentication
// results use request.Generation. Artwork uses imageID. Adjacent media uses
// mediaGeneration. Fields belonging to other kinds are ignored. Workers must
// stop mutating referenced data before sending a result.
type result struct {
	request         Request
	page            jellyfin.Page
	err             error
	client          *jellyfin.Client
	code            string
	kind            resultKind
	update          artUpdate
	imageID         int
	mediaGeneration int
	parent          View
	item            *jellyfin.Item
}

func (s *browserSession) handleResult(r result) bool {
	switch r.kind {
	case authResult:
		return s.handleAuth(r)
	case artworkResult:
		return s.handleArtwork(r)
	case neighborResult:
		return s.handleNeighbor(r)
	default:
		return s.handlePage(r)
	}
}
