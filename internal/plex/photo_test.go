package plex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"reflect"
	"testing"

	"misterfin-crt/internal/media"
)

func TestPhotoLibraryHierarchy(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/playlists", "/library/all":
			fmt.Fprint(w, `{"MediaContainer":{"size":0,"totalSize":0}}`)
		case "/library/sections":
			fmt.Fprint(w, `{"MediaContainer":{"Directory":[{"key":"4","title":"Family Pictures","type":"photo"}]}}`)
		case "/livetv/dvrs":
			fmt.Fprint(w, `{"MediaContainer":{"Dvr":[]}}`)
		case "/library/sections/4/all":
			if r.URL.Query().Get("sort") != "titleSort:asc" {
				t.Error("missing stable library order")
			}
			fmt.Fprint(w, `{"MediaContainer":{"totalSize":1,"Metadata":[{"ratingKey":"40","type":"photo","key":"/library/metadata/40/children","title":"Vacation","thumb":"/library/metadata/40/thumb/1"}]}}`)
		case "/library/metadata/40/children":
			if r.URL.Query().Get("X-Plex-Container-Start") != "64" || r.URL.Query().Get("X-Plex-Container-Size") != "64" {
				t.Error("photo paging lost")
			}
			fmt.Fprint(w, `{"MediaContainer":{"totalSize":67,"offset":64,"Metadata":[{"ratingKey":"41","type":"photo","key":"/library/metadata/41/children","title":"Day two"},{"ratingKey":"42","type":"photo","title":"Beach","thumb":"/library/metadata/42/thumb/1","Media":[{"id":8,"Part":[{"id":9,"key":"/library/parts/9/1/file.jpg"}]}]},{"ratingKey":"43","type":"clip","title":"Waves"}]}}`)
		case "/library/metadata/41/children":
			fmt.Fprint(w, `{"MediaContainer":{"totalSize":1,"Photo":[{"ratingKey":"44","type":"photo","title":"Sunset","thumb":"/library/metadata/44/thumb/1"}]}}`)
		case "/library/metadata/42":
			fmt.Fprint(w, `{"MediaContainer":{"Photo":[{"ratingKey":"42","type":"photo","title":"Beach","thumb":"/library/metadata/42/thumb/1"}]}}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})
	libraries, err := c.Libraries(t.Context())
	if err != nil || len(libraries.Items) != 1 || libraries.Items[0].Name != "Family Pictures" || libraries.Items[0].CollectionType != "photos" {
		t.Fatalf("photo library: %+v, %v", libraries, err)
	}
	library := libraries.Items[0]
	page, err := c.List(t.Context(), media.Location{ParentID: library.ID, Collection: "photos"}, 0, 64)
	if err != nil || len(page.Items) != 1 || page.Items[0].Type != "PhotoAlbum" || !page.Items[0].IsFolder {
		t.Fatalf("photo album: %+v, %v", page, err)
	}
	count, err := c.LibraryCount(t.Context(), library)
	if err != nil || count == nil || *count != 1 {
		t.Fatalf("library count: %v, %v", count, err)
	}
	mosaic, err := c.Mosaic(t.Context(), library)
	if err != nil || !reflect.DeepEqual(mosaic.Items, page.Items) {
		t.Fatalf("mosaic: %+v, %v", mosaic, err)
	}
	page, err = c.List(t.Context(), media.Location{ParentID: "40", Collection: "photos"}, 64, 64)
	if err != nil || len(page.Items) != 3 || page.TotalRecordCount == nil || *page.TotalRecordCount != 67 {
		t.Fatalf("album contents: %+v, %v", page, err)
	}
	if page.Items[0].Type != "PhotoAlbum" || !page.Items[0].IsFolder || page.Items[1].Type != "Photo" || page.Items[1].IsFolder || page.Items[2].Type != "Video" || page.Items[2].IsFolder {
		t.Fatalf("mixed album types: %+v", page.Items)
	}
	if page.Items[1].ImageTags["Primary"] != "/library/parts/9/1/file.jpg" {
		t.Fatal("viewer must resize the original photo")
	}
	page, err = c.List(t.Context(), media.Location{ParentID: "41", Collection: "photos"}, 0, 64)
	if err != nil || len(page.Items) != 1 || page.Items[0].Type != "Photo" {
		t.Fatalf("nested album: %+v, %v", page, err)
	}
	detail, err := c.Details(t.Context(), "42")
	if err != nil || detail.Type != "Photo" || detail.ID != "42" {
		t.Fatalf("photo details: %+v, %v", detail, err)
	}
}

func TestPhotoAlbumMetadata(t *testing.T) {
	var response containerResponse
	err := json.Unmarshal([]byte(`{"MediaContainer":{"Metadata":[{"ratingKey":"1","type":"photoalbum"}],"Directory":[{"ratingKey":"2","type":"photo","title":"Album"},{"key":"all","title":"All photos"}],"Photo":[{"ratingKey":"3","type":"photo","parentThumb":"/album/thumb","grandparentThumb":"/library/thumb"}]}}`), &response)
	if err != nil {
		t.Fatal(err)
	}
	entries := response.Container.entries()
	if len(entries) != 3 {
		t.Fatalf("media entries = %d", len(entries))
	}
	for _, entry := range entries[:2] {
		if item := entry.item(); item.Type != "PhotoAlbum" || !item.IsFolder {
			t.Fatalf("album mapping: %+v", item)
		}
	}
	if item := entries[2].item(); item.Type != "Photo" || item.IsFolder || item.ImageTags["Primary"] != "" {
		t.Fatalf("missing photo must not show its parent's artwork: %+v", item)
	}
}

func TestPhotoFitAndFailures(t *testing.T) {
	for _, height := range []int{240, 480} {
		t.Run(fmt.Sprint(height), func(t *testing.T) {
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				q := r.URL.Query()
				if r.URL.Path != "/photo/:/transcode" || q.Get("url") != "/library/parts/9/1/file.jpg" || q.Get("width") != "640" || q.Get("height") != fmt.Sprint(height) || q.Get("minSize") != "0" || q.Get("upscale") != "0" {
					t.Error("incorrect photo request")
				}
				// Even if the server returns a larger image, decoding keeps it bounded.
				png.Encode(w, image.NewRGBA(image.Rect(0, 0, 600, 800)))
			})
			item := media.Item{Type: "Photo", ImageTags: map[string]string{"Primary": "/library/parts/9/1/file.jpg"}}
			im, err := c.Photo(t.Context(), item, 640, height)
			if err != nil || im.Bounds().Size() != image.Pt(height*3/4, height) {
				t.Fatalf("photo fit: %v, %v", im, err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			if _, err := c.Photo(ctx, item, 640, height); !errors.Is(err, context.Canceled) {
				t.Fatalf("canceled photo: %v", err)
			}
		})
	}
	c := testClient(t, func(http.ResponseWriter, *http.Request) { t.Error("invalid photo made a request") })
	for _, path := range []string{"", "//elsewhere/image", "https://elsewhere/image", "/image?private-token", "/image#fragment"} {
		if _, err := c.Photo(t.Context(), media.Item{Type: "Photo", ImageTags: map[string]string{"Primary": path}}, 640, 480); err == nil {
			t.Errorf("accepted invalid image reference %q", path)
		}
	}
	item := media.Item{Type: "Photo", ImageTags: map[string]string{"Primary": "/image"}}
	for _, size := range []image.Point{{0, 480}, {640, -1}, {2049, 480}, {640, 2049}} {
		if _, err := c.Photo(t.Context(), item, size.X, size.Y); err == nil {
			t.Errorf("accepted invalid bounds %v", size)
		}
	}
	item.Type = "PhotoAlbum"
	if _, err := c.Photo(t.Context(), item, 640, 480); err == nil {
		t.Fatal("opened an album as a photo")
	}
	for _, status := range []int{200, 404, 500} {
		t.Run(fmt.Sprintf("failure-%d", status), func(t *testing.T) {
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				fmt.Fprint(w, "not an image")
			})
			item := media.Item{Type: "Photo", ImageTags: map[string]string{"Primary": "/image"}}
			if _, err := c.Photo(t.Context(), item, 640, 480); err == nil {
				t.Fatal("accepted unavailable or corrupt photo")
			}
		})
	}
}
