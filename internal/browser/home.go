package browser

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"misterfin-crt/internal/media"
)

const continueID = "misterfin-crt:continue"

// homeState owns the combined Continue Watching snapshot and its independent
// request. Library browsing never waits for this request or cancels it.
type homeState struct {
	cancel          context.CancelFunc
	generation      int
	items           []media.Item
	err             error
	loading, loaded bool
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
		s.send(work, homeResult{generation: generation, page: page, err: err})
	}()
}

func (s *browserSession) handleHome(r homeResult) bool {
	if r.generation != s.home.generation {
		return false
	}
	s.home.loading = false
	s.home.loaded = true
	s.home.err = r.err
	// Keep a useful previous snapshot if a refresh fails completely.
	if r.err == nil || len(r.page.Items) > 0 {
		s.home.items = r.page.Items
	}
	if errors.Is(r.err, media.ErrUnauthorized) {
		s.setup = s.setupPresentation(r.err)
	}
	s.syncHomeViews()
	s.config.Diagnostics.Record("browser.home", slog.Int("items", len(s.home.items)), slog.Bool("failed", r.err != nil))
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
			replaceHomePage(v, media.Page{Items: s.home.items, TotalRecordCount: &total}, s.model.Rows)
			v.Loading = s.home.loading && !s.home.loaded
			v.fetching = false
			v.Error = ""
			if s.home.err != nil {
				v.Error = "Some Continue Watching items could not load."
			}
		}
	}
}

func (s *browserSession) homeLibraries(page media.Page) media.Page {
	items := make([]media.Item, 0, len(page.Items)+1)
	// Reserve Continue before its first response so startup never presents a
	// library and then changes focus when the slower feed arrives.
	if !s.home.loaded || len(s.home.items) > 0 || s.home.err != nil {
		items = append(items, media.Item{ID: continueID, Name: "Continue", Type: "Folder", IsFolder: true})
	}
	for _, item := range page.Items {
		if item.CollectionType == "boxsets" && s.config.ShowCollections != nil && !*s.config.ShowCollections {
			continue
		}
		if item.CollectionType == "playlists" && s.config.ShowPlaylists != nil && !*s.config.ShowPlaylists {
			continue
		}
		if item.ID != continueID && !unsupportedLibrary(item) {
			items = append(items, item)
		}
	}
	total := len(items)
	return media.Page{Items: items, TotalRecordCount: &total}
}

func replaceHomePage(v *View, page media.Page, rows int) {
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
	if s.selection.loader == nil || !s.home.loaded {
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

// unsupportedLibrary excludes reading formats by server category, never by name.
// Plex has no audiobook category. Audio stored as ordinary music remains music.
func unsupportedLibrary(item media.Item) bool {
	switch strings.ToLower(item.CollectionType) {
	case "books", "book", "comics", "comic", "audiobooks", "audiobook":
		return true
	}
	return item.Type == "Book" || item.Type == "AudioBook"
}
