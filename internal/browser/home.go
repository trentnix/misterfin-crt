package browser

import (
	"context"
	"time"

	"misterfin-go/internal/jellyfin"
)

const continueID = "misterfin-go:continue"

// homeState owns the combined Continue Watching snapshot and its independent
// request. Library browsing never waits for this request or cancels it.
type homeState struct {
	cancel          context.CancelFunc
	generation      int
	items           []jellyfin.Item
	err             error
	loading, loaded bool
	initialFocus    bool
}

func (s *browserSession) refreshHome() {
	if s.client == nil {
		return
	}
	if s.home.cancel != nil {
		s.home.cancel()
	}
	s.home.generation++
	generation := s.home.generation
	work, stop := context.WithCancel(s.ctx)
	s.home.cancel = stop
	s.home.loading = true
	if v := s.model.Current(); v.Location.Kind == "continue" && v.Detail == nil {
		v.Loading = len(v.Page.Items) == 0
		v.Error = ""
	}
	client := s.client
	go func() {
		page, err := client.ContinueWatching(work)
		s.send(work, result{kind: homeResult, homeGeneration: generation, page: page, err: err})
	}()
}

func (s *browserSession) handleHome(r result) bool {
	if r.homeGeneration != s.home.generation {
		return false
	}
	s.home.loading = false
	s.home.loaded = true
	s.home.err = r.err
	// Keep a useful previous snapshot if a refresh fails completely.
	if r.err == nil || len(r.page.Items) > 0 {
		s.home.items = r.page.Items
	}
	if jellyfin.Rejected(r.err) {
		s.status = "Session rejected. Press R to sign in again."
	}
	s.syncHomeViews()
	if s.home.initialFocus && len(s.model.Stack) == 1 && len(s.home.items) > 0 {
		v := s.model.Current()
		v.Selected = 0
		v.Target = 0
		v.centerSelection(s.model.Rows)
	}
	s.home.initialFocus = false
	s.seedHomeArtwork()
	if item := s.model.Current().Item(); item != nil && item.ID == continueID {
		s.selection.key = ""
	}
	s.loadSelection()
	return true
}

// syncHomeViews updates retained screens by item identity. A refresh can move
// an episode or remove a completed movie without jumping to the first row.
func (s *browserSession) syncHomeViews() {
	for i := range s.model.Stack {
		v := &s.model.Stack[i]
		switch v.Location.Kind {
		case "views":
			if v.Page.Items != nil {
				replaceHomePage(v, s.homeLibraries(v.Page), s.model.Rows)
			}
		case "continue":
			if v.Detail != nil {
				continue
			}
			total := len(s.home.items)
			replaceHomePage(v, jellyfin.Page{Items: s.home.items, TotalRecordCount: &total}, s.model.Rows)
			v.Loading = s.home.loading && !s.home.loaded
			v.fetching = false
			v.Error = ""
			if s.home.err != nil {
				v.Error = "Some Continue Watching items could not load. R:retry"
			}
		}
	}
}

func (s *browserSession) homeLibraries(page jellyfin.Page) jellyfin.Page {
	items := make([]jellyfin.Item, 0, len(page.Items)+1)
	if len(s.home.items) > 0 || s.home.err != nil {
		items = append(items, jellyfin.Item{ID: continueID, Name: "Continue", Type: "Folder", IsFolder: true})
	}
	for _, item := range page.Items {
		if item.ID != continueID {
			items = append(items, item)
		}
	}
	total := len(items)
	return jellyfin.Page{Items: items, TotalRecordCount: &total}
}

func replaceHomePage(v *View, page jellyfin.Page, rows int) {
	id, series := "", ""
	if item := v.Item(); item != nil {
		id, series = item.ID, item.SeriesID
	}
	selected := -1
	for i, item := range page.Items {
		if item.ID == id {
			selected = i
			break
		}
	}
	if selected < 0 && series != "" {
		for i, item := range page.Items {
			if item.SeriesID == series {
				selected = i
				break
			}
		}
	}
	if selected < 0 {
		selected = min(v.Selected, max(0, len(page.Items)-1))
	}
	v.Page = page
	v.Start = 0
	v.Selected = selected
	v.Target = selected
	v.centerSelection(rows)
}

func (s *browserSession) seedHomeArtwork() {
	if s.selection.loader == nil {
		return
	}
	total := len(s.home.items)
	items := s.home.items[:min(12, len(s.home.items))]
	s.selection.loader.libraries.remember(continueID, func(lib *cachedLibrary) {
		lib.count = &total
		lib.items = items
		lib.countUntil = time.Now().Add(libraryCacheTTL)
		lib.itemsUntil = lib.countUntil
	})
}

func (s *browserSession) loadContinue() {
	s.syncHomeViews()
	s.loadSelection()
	if !s.home.loading {
		s.refreshHome()
	}
}
