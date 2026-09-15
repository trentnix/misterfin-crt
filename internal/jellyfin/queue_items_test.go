package jellyfin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"testing"
)

func TestAudioQueueUsesServerOffsetsBeforeFiltering(t *testing.T) {
	for _, knownTotal := range []bool{false, true} {
		t.Run(strconv.FormatBool(knownTotal), func(t *testing.T) {
			var offsets []int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				q := r.URL.Query()
				start, _ := strconv.Atoi(q.Get("StartIndex"))
				offsets = append(offsets, start)
				if r.URL.Path != "/Items" || q.Get("ParentId") != "album" || q.Get("Limit") != "200" || q.Get("SortBy") != "SortName" {
					t.Error("queue changed the listing query")
				}
				var items []Item
				for i := start; i < min(start+200, 203); i++ {
					kind := "Audio"
					if i < 200 && i%2 != 0 {
						kind = "Folder"
					}
					items = append(items, Item{ID: strconv.Itoa(i), Type: kind})
				}
				page := Page{Items: items}
				if knownTotal {
					total := 203
					page.TotalRecordCount = &total
				}
				_ = json.NewEncoder(w).Encode(page)
			}))
			defer server.Close()
			client := NewClient(Config{Server: server.URL}, Session{UserID: "user"})
			items, err := client.AudioQueue(context.Background(), Location{Kind: "items", ParentID: "album", Collection: "music"})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(offsets, []int{0, 200}) || len(items) != 103 || items[99].ID != "198" || items[100].ID != "200" || items[102].ID != "202" {
				t.Fatalf("wrong filtered queue: offsets %v, tracks %d", offsets, len(items))
			}
		})
	}
}

func TestQueuePagesRejectOversizeAndPartialResults(t *testing.T) {
	for _, total := range []int{10000, 10001} {
		t.Run(strconv.Itoa(total), func(t *testing.T) {
			items, err := collectQueuePages(func(start, limit int) (Page, error) {
				return Page{Items: make([]Item, min(limit, total-start)), TotalRecordCount: &total}, nil
			})
			if total == 10000 {
				if err != nil || len(items) != total {
					t.Fatal("rejected queue at limit", err)
				}
			} else if err == nil || items != nil {
				t.Fatal("accepted oversized or partial queue")
			}
		})
	}
	sentinel := errors.New("second page failed")
	items, err := collectQueuePages(func(start, limit int) (Page, error) {
		if start > 0 {
			return Page{}, sentinel
		}
		return Page{Items: make([]Item, limit)}, nil
	})
	if !errors.Is(err, sentinel) || items != nil {
		t.Fatal("returned partial queue after failure")
	}
}
