package jellyfin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestRemoteItemsPreserveOrderDuplicatesAndExpandAlbum(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var items []Item
		switch r.URL.Query().Get("Ids") {
		case "b,a,b":
			items = []Item{{ID: "a", Type: "Audio"}, {ID: "b", Type: "Audio"}}
		case "album":
			items = []Item{{ID: "album", Type: "MusicAlbum", IsFolder: true}}
		default:
			if r.URL.Query().Get("ParentId") == "album" {
				items = []Item{{ID: "c", Type: "Audio"}, {ID: "d", Type: "Audio"}}
			} else {
				items = []Item{}
			}
		}
		total := len(items)
		_ = json.NewEncoder(w).Encode(Page{Items: items, TotalRecordCount: &total})
	}))
	defer server.Close()
	client := NewClient(Config{Server: server.URL}, Session{UserID: "user"})
	items, err := client.RemoteItems(context.Background(), []string{"b", "a", "b"}, false)
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	if !reflect.DeepEqual(ids, []string{"b", "a", "b"}) {
		t.Fatal(ids)
	}
	items, err = client.RemoteItems(context.Background(), []string{"album"}, false)
	if err != nil || len(items) != 2 || items[0].ID != "c" {
		t.Fatal(items, err)
	}
	if _, err = client.RemoteItems(context.Background(), []string{"missing"}, false); err == nil {
		t.Fatal("missing items accepted")
	}
}
