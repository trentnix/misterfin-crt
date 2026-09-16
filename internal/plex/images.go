package plex

import (
	"context"
	"errors"
	"image"
	"net/url"
	"strconv"

	"misterfin-crt/internal/artwork/bitmap"
	"misterfin-crt/internal/media"
)

// ImageKind retrieves server-sized artwork without exposing private URLs.
func (c *Client) ImageKind(ctx context.Context, item media.Item, kind string) (image.Image, error) {
	path := item.ImageTags[kind]
	width := 320
	if kind == "Backdrop" {
		width = 640
		if len(item.BackdropImageTags) > 0 {
			path = item.BackdropImageTags[0]
		}
	}
	if path == "" {
		return nil, nil
	}
	return c.image(ctx, path, width, 360)
}

// Photo is unavailable while the Plex adapter exposes video libraries only.
func (c *Client) Photo(context.Context, media.Item, int, int) (image.Image, error) {
	return nil, errors.New("Plex photos are not supported yet")
}

func (c *Client) image(ctx context.Context, path string, width, height int) (image.Image, error) {
	u, err := url.Parse(path)
	if err != nil || u.IsAbs() || u.Host != "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || len(path) == 0 || path[0] != '/' {
		return nil, errors.New("invalid Plex image path")
	}
	data, err := c.request(ctx, "GET", "/photo/:/transcode", url.Values{"url": {path}, "width": {strconv.Itoa(width)}, "height": {strconv.Itoa(height)}, "minSize": {"0"}, "upscale": {"0"}})
	if err != nil {
		return nil, err
	}
	return bitmap.Decode(data, width, height)
}
