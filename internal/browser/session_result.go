package browser

import (
	"mistervision/internal/connection"
	"mistervision/internal/media"
	"mistervision/internal/musicviz"
)

// workerResult is one completed outcome delivered to the browser event loop.
// Its unexported method limits implementations to this package. Each concrete
// result carries only the values its handler can consume. Producers must stop
// mutating referenced payloads before sending a result.
type workerResult interface {
	apply(*browserSession) bool
}

type pageResult struct {
	connectionGeneration int // Authentication scope, independent of model navigation.
	request              Request
	page                 media.Page
	err                  error
}

func (r pageResult) apply(s *browserSession) bool { return s.handlePage(r) }

type authCodeResult struct {
	generation   int
	presentation connection.Presentation
}

func (r authCodeResult) apply(s *browserSession) bool { return s.handleAuthCode(r) }

type authResult struct {
	generation int
	connection *authenticatedConnection
	err        error
}

func (r authResult) apply(s *browserSession) bool { return s.handleAuth(r) }

type selectionResult struct {
	generation int
	update     selectionUpdate
}

func (r selectionResult) apply(s *browserSession) bool { return s.handleSelection(r) }

type neighborResult struct {
	generation int
	parent     View
	item       *media.Item
	err        error
}

func (r neighborResult) apply(s *browserSession) bool { return s.handleNeighbor(r) }

type homeResult struct {
	generation int
	page       media.Page
	err        error
}

func (r homeResult) apply(s *browserSession) bool { return s.handleHome(r) }

type shuffleResult struct {
	generation int
	page       media.Page
	err        error
}

func (r shuffleResult) apply(s *browserSession) bool { return s.handleShuffle(r) }

type musicAssetsResult struct {
	music *musicviz.Library
	index int
	err   error
}

func (r musicAssetsResult) apply(s *browserSession) bool { return s.handleMusicAssets(r) }

func (s *browserSession) handleResult(r workerResult) bool {
	return r.apply(s)
}
