package jellyfin

import (
	"bytes"
	"context"
	"errors"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"net/url"
	"strconv"
)

// Image fetches primary artwork. A missing image tag returns nil without an error.
func (c *Client) Image(ctx context.Context, item Item) (image.Image, error) {
	return c.ImageKind(ctx, item, "Primary")
}

// ImageKind fetches tagged artwork such as Primary, Backdrop, or Logo.
// Backdrops can fall back to parent artwork. Missing tags return nil without
// a request. The caller owns the decoded image and any caching.
func (c *Client) ImageKind(ctx context.Context, item Item, kind string) (image.Image, error) {
	return c.imageSized(ctx, item, kind, 0, 360, 80)
}

// Photo fetches the primary image sized for logical framebuffer dimensions.
// Width and height must be between 1 and 2048. A missing primary tag is an error.
// Decoding preserves aspect ratio and bounds the returned image dimensions.
func (c *Client) Photo(ctx context.Context, item Item, width, height int) (image.Image, error) {
	if width < 1 || height < 1 || width > 2048 || height > 2048 || item.ImageTags["Primary"] == "" {
		return nil, errors.New("photo unavailable")
	}
	return c.imageSized(ctx, item, "Primary", width, height, 90)
}

func (c *Client) imageSized(ctx context.Context, item Item, kind string, requestedWidth, maxHeight, quality int) (image.Image, error) {
	tag := item.ImageTags[kind]
	if kind == "Backdrop" && len(item.BackdropImageTags) == 0 && len(item.ParentBackdropImageTags) > 0 {
		item.ID = item.ParentBackdropItemID
		item.BackdropImageTags = item.ParentBackdropImageTags
	}
	if kind == "Backdrop" && len(item.BackdropImageTags) > 0 {
		tag = item.BackdropImageTags[0]
	}
	if tag == "" {
		return nil, nil
	}
	format := "Jpg"
	if kind == "Logo" {
		format = "Png"
	}
	path := kind
	if kind == "Backdrop" {
		path += "/0"
	}
	width := "320"
	if kind == "Backdrop" {
		width = "640"
	}
	if kind == "Logo" {
		width = "480"
	}
	if requestedWidth > 0 {
		width = strconv.Itoa(requestedWidth)
	}
	b, err := c.request(ctx, "GET", "/Items/"+url.PathEscape(item.ID)+"/Images/"+path, url.Values{"tag": {tag}, "maxWidth": {width}, "maxHeight": {strconv.Itoa(maxHeight)}, "quality": {strconv.Itoa(quality)}, "format": {format}}, nil)
	if err != nil {
		return nil, err
	}
	maxWidth, _ := strconv.Atoi(width)
	return decodeArtwork(b, maxWidth, maxHeight)
}

// decodeArtwork bounds decoded and cached dimensions even when a server ignores
// requested sizes. The nearest-neighbor reduction preserves the existing cache
// cost and pixel behavior.
func decodeArtwork(b []byte, maxWidth, maxHeight int) (image.Image, error) {
	conf, _, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil || conf.Width < 1 || conf.Height < 1 || conf.Width > 2048 || conf.Height > 2048 {
		return nil, errors.New("invalid or oversized artwork")
	}
	im, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	scale := min(1.0, min(float64(maxWidth)/float64(conf.Width), float64(maxHeight)/float64(conf.Height)))
	if scale < 1 {
		resized := image.NewRGBA(image.Rect(0, 0, max(1, int(float64(conf.Width)*scale)), max(1, int(float64(conf.Height)*scale))))
		bounds := im.Bounds()
		for y := 0; y < resized.Bounds().Dy(); y++ {
			for x := 0; x < resized.Bounds().Dx(); x++ {
				resized.Set(x, y, im.At(bounds.Min.X+x*bounds.Dx()/resized.Bounds().Dx(), bounds.Min.Y+y*bounds.Dy()/resized.Bounds().Dy()))
			}
		}
		im = resized
	}
	return im, nil
}
