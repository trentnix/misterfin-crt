package jellyfin

import (
	"context"
	"errors"
	"image"
	"net/url"
	"strconv"

	"mistervision/internal/artwork/bitmap"
)

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
	return bitmap.Decode(b, maxWidth, maxHeight)
}
