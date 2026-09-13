package jellyfin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRandomTracksScopesAndBoundsBatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.URL.Path != "/Items" || q.Get("ParentId") != "music-library" || q.Get("Recursive") != "true" || q.Get("IncludeItemTypes") != "Audio" || q.Get("SortBy") != "Random" || q.Get("Limit") != "64" {
			t.Errorf("unexpected query %s", r.URL)
		}
		json.NewEncoder(w).Encode(Page{Items: []Item{{ID: "one", Type: "Audio"}, {ID: "one", Type: "Audio"}, {ID: "two", Type: "Audio"}}})
	}))
	defer server.Close()
	c := NewClient(Config{Server: server.URL}, Session{UserID: "user", Token: "token"})
	items, err := c.RandomTracks(context.Background(), "music-library")
	if err != nil || len(items) != 2 {
		t.Fatal(items, err)
	}
}
