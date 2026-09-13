package browser

import (
	"misterfin-crt/internal/jellyfin"
	"misterfin-crt/internal/musicviz"
)

// resultKind identifies the worker that produced a result. Request generation
// checks remain with each handler, beside the state they protect.
type resultKind uint8

const (
	pageResult resultKind = iota
	authResult
	selectionResult
	neighborResult
	homeResult
	shuffleResult
	musicConfigResult
	musicAssetsResult
)

// result carries one worker result, selected by kind. Page and authentication
// results use request.Generation. Selection uses selectionGeneration. Adjacent media uses
// mediaGeneration. Home refreshes use homeGeneration. Fields for other kinds are ignored. Workers must
// stop mutating referenced data before sending a result.
type result struct {
	music               *musicviz.Library
	musicIndex          int
	request             Request
	page                jellyfin.Page
	err                 error
	client              *jellyfin.Client
	code                string
	kind                resultKind
	update              selectionUpdate
	selectionGeneration int
	mediaGeneration     int
	homeGeneration      int
	parent              View
	item                *jellyfin.Item
}

func (s *browserSession) handleResult(r result) bool {
	switch r.kind {
	case musicAssetsResult:
		return s.handleMusicAssets(r)
	case musicConfigResult:
		return s.handleMusicConfig(r)
	case shuffleResult:
		return s.handleShuffle(r)
	case homeResult:
		return s.handleHome(r)
	case authResult:
		return s.handleAuth(r)
	case selectionResult:
		return s.handleSelection(r)
	case neighborResult:
		return s.handleNeighbor(r)
	default:
		return s.handlePage(r)
	}
}
