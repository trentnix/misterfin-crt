package browser

import (
	"errors"
	"testing"

	"mistervision/internal/input/control"
	"mistervision/internal/media"
)

func homeEpisode(id, action string) media.Item {
	season, episode := 1, 4
	return media.Item{ID: id, Name: "Episode name", Type: "Episode", SeriesID: "series", SeriesName: "Dungeons and Dragons", ParentIndexNumber: &season, IndexNumber: &episode, ContinueAction: action}
}

func TestHomeCardAndListNavigation(t *testing.T) {
	s := testSession(t)
	s.home.items = []media.Item{homeEpisode("episode", "next")}
	s.home.loaded = true
	s.model.Current().Page = media.Page{Items: []media.Item{{ID: "movies", Name: "Movies"}}}
	s.syncHomeViews()
	if len(s.model.Current().Page.Items) != 2 || s.model.Current().Item().ID != "movies" {
		t.Fatal("home arrival moved library selection")
	}
	s.model.Key(control.Previous)
	req := s.model.Key(control.Open)
	if req == nil || req.Location.Kind != "continue" || s.model.Current().Title != "Continue Watching" {
		t.Fatal(req, s.model.Current())
	}
	s.syncHomeViews()
	if s.model.Current().Loading || *s.model.Current().Page.TotalRecordCount != 1 {
		t.Fatal(s.model.Current())
	}
	if req = s.model.Key(control.Open); req != nil || s.model.Current().Detail.ID != "episode" {
		t.Fatal("home episode did not open existing details")
	}
	s.model.ReturnToParent()
	s.home.items = []media.Item{homeEpisode("next-episode", "next")}
	s.syncHomeViews()
	if s.model.Current().Item().ID != "next-episode" {
		t.Fatal("series replacement lost selection")
	}
	s.model.ReturnToParent()
	s.home.items = nil
	s.syncHomeViews()
	if len(s.model.Current().Page.Items) != 1 || s.model.Current().Item().ID != "movies" {
		t.Fatal("empty home card did not disappear")
	}
}

func TestHomeRefreshRejectsStaleResultsAndKeepsUsefulData(t *testing.T) {
	s := testSession(t)
	s.home.generation = 2
	s.home.items = []media.Item{homeEpisode("episode", "resume")}
	if s.handleHome(homeResult{generation: 1}) || len(s.home.items) != 1 {
		t.Fatal("stale home result accepted")
	}
	// No client means loadSelection cannot start network work in this fixture.
	if !s.handleHome(homeResult{generation: 2, err: errors.New("offline")}) || len(s.home.items) != 1 {
		t.Fatal("failed refresh erased usable data")
	}
	if !s.handleHome(homeResult{generation: 2, page: media.Page{Items: []media.Item{}}}) || len(s.home.items) != 0 {
		t.Fatal("successful empty refresh retained old entries")
	}
}

func TestHomeInitialCardDoesNotStealNavigation(t *testing.T) {
	for _, navigate := range []bool{true, false} {
		s := testSession(t)
		s.home.loading = true
		req := s.model.Load(0)
		s.handlePage(pageResult{request: *req, page: media.Page{Items: []media.Item{{ID: "movies", Name: "Movies"}}}})
		if s.model.Current().Item().ID != continueID {
			t.Fatal("first carousel frame did not select Continue")
		}
		if navigate {
			s.model.Key(control.Next)
		}
		s.handleHome(homeResult{page: media.Page{Items: []media.Item{homeEpisode("episode", "next")}}})
		want := continueID
		if navigate {
			want = "movies"
		}
		if s.model.Current().Item().ID != want {
			t.Fatal("unexpected home selection", s.model.Current().Item())
		}
	}
}

func TestHomeFeedCanArriveBeforeLibraries(t *testing.T) {
	s := testSession(t)
	req := s.model.Load(0)
	s.handleHome(homeResult{page: media.Page{Items: []media.Item{homeEpisode("episode", "next")}}})
	s.handlePage(pageResult{request: *req, page: media.Page{Items: []media.Item{{ID: "movies", Name: "Movies"}}}})
	if s.model.Current().Item().ID != continueID {
		t.Fatal("completed feed was not selected on the first carousel frame")
	}
}

func TestInitialContinueCanOpenWhileLoading(t *testing.T) {
	s := testSession(t)
	s.home.loading = true
	s.model.Current().Page = s.homeLibraries(media.Page{Items: []media.Item{{ID: "movies", Name: "Movies"}}})
	req := s.model.Key(control.Open)
	if req == nil || req.Location.Kind != "continue" {
		t.Fatal("initial placeholder did not open Continue")
	}
	s.syncHomeViews()
	if !s.model.Current().Loading {
		t.Fatal("pending Continue list did not indicate loading")
	}
	s.handleHome(homeResult{page: media.Page{Items: []media.Item{homeEpisode("episode", "next")}}})
	if s.model.Current().Loading || s.model.Current().Item().ID != "episode" {
		t.Fatal("pending Continue list did not receive the feed")
	}
}

func TestEmptyInitialContinueRemovesPlaceholder(t *testing.T) {
	for _, navigate := range []bool{false, true} {
		s := testSession(t)
		s.model.Current().Page = s.homeLibraries(media.Page{Items: []media.Item{{ID: "movies", Name: "Movies"}}})
		if navigate {
			s.model.Key(control.Next)
		}
		s.handleHome(homeResult{page: media.Page{Items: []media.Item{}}})
		if len(s.model.Current().Page.Items) != 1 || s.model.Current().Item().ID != "movies" {
			t.Fatal("empty Continue did not leave the library selected")
		}
	}
}

func TestPendingContinueDoesNotPublishZeroCount(t *testing.T) {
	s := testSession(t)
	s.selection.loader = newSelectionLoader(nil, 640, 240, selectionCaches{})
	s.seedHomeArtwork()
	if s.selection.loader.libraries.cached(continueID).count != nil {
		t.Fatal("pending feed was presented as an empty feed")
	}
	s.home.loaded = true
	s.home.items = []media.Item{homeEpisode("episode", "next")}
	s.seedHomeArtwork()
	count := s.selection.loader.libraries.cached(continueID).count
	if count == nil || *count != 1 {
		t.Fatal("completed feed count was not published")
	}
}
