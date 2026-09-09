package jellyfin

import (
	"context"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"
)

func TestPhotoQueryAndBounds(t *testing.T) {
	for _, height := range []int{240, 288} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			want := url.Values{"tag": {"tag"}, "maxWidth": {"640"}, "maxHeight": {map[int]string{240: "240", 288: "288"}[height]}, "quality": {"90"}, "format": {"Jpg"}}
			if r.URL.Path != "/Items/photo/Images/Primary" || !reflect.DeepEqual(r.URL.Query(), want) {
				t.Error("photo query differs from C")
			}
			png.Encode(w, image.NewRGBA(image.Rect(0, 0, 800, 600)))
		}))
		c := NewClient(Config{Server: server.URL}, Session{})
		im, err := c.Photo(context.Background(), Item{ID: "photo", ImageTags: map[string]string{"Primary": "tag"}}, 640, height)
		server.Close()
		if err != nil {
			t.Fatal(err)
		}
		if im.Bounds().Dx() > 640 || im.Bounds().Dy() > height {
			t.Fatal("ignored bounds")
		}
	}
}
