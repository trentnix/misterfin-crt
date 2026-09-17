package plex

import (
	"context"
	"errors"
	"image"
	"net/url"
	"strconv"

	"mistervision/internal/artwork/bitmap"
	"mistervision/internal/media"
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

// Photo retrieves an image fitted to the logical framebuffer. Plex applies EXIF
// orientation and converts the source before download. Bounds must be 1–2048.
func (c *Client) Photo(ctx context.Context, item media.Item, width, height int) (image.Image, error) {
	if item.Type != "Photo" || width < 1 || height < 1 || width > 2048 || height > 2048 || item.ImageTags["Primary"] == "" {
		return nil, errors.New("photo unavailable")
	}
	return c.image(ctx, item.ImageTags["Primary"], width, height)
}

func (c *Client) image(ctx context.Context, path string, width, height int) (image.Image, error) {
	u, err := url.Parse(path)
	relative := err == nil && !u.IsAbs() && u.Host == "" && len(path) > 0 && path[0] == '/'
	// Channel logos are hosted by Plex. Ask the configured server to resize them,
	// keeping our authenticated HTTP request on the server's origin.
	plexLogo := err == nil && u.Scheme == "https" && (u.Host == "provider-static.plex.tv" || u.Host == "plex.tmsimg.com")
	if err != nil || (!relative && !plexLogo) || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("invalid Plex image path")
	}
	data, err := c.request(ctx, "GET", "/photo/:/transcode", url.Values{"url": {path}, "width": {strconv.Itoa(width)}, "height": {strconv.Itoa(height)}, "minSize": {"0"}, "upscale": {"0"}})
	if err != nil {
		return nil, err
	}
	return bitmap.Decode(data, width, height)
}
