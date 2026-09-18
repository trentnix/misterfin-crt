package jellyfin

import (
	"context"
	"errors"
	"image"
	"net/url"

	"mistervision/internal/artwork/bitmap"
)

// User identifies the authenticated viewer and their optional avatar.
// It contains no credentials or server administration settings.
type User struct {
	ID              string `json:"Id"`
	Name            string `json:"Name"`
	PrimaryImageTag string `json:"PrimaryImageTag,omitempty"`
}

// CurrentUser verifies that the access token belongs to the selected viewer.
// API keys do not belong to a user and must not use this endpoint.
func (c *Client) CurrentUser(ctx context.Context) (User, error) {
	var user User
	if err := c.json(ctx, "GET", "/Users/Me", nil, nil, &user); err != nil {
		return User{}, err
	}
	if user.ID == "" || user.ID != c.Session.UserID || user.Name == "" {
		return User{}, errors.New("invalid Jellyfin user identity")
	}
	return user, nil
}

// UserAvatar fetches the authenticated viewer's image. A missing image tag
// avoids a request. Image decoding and HTTP response sizes are bounded.
func (c *Client) UserAvatar(ctx context.Context, tag string) (image.Image, error) {
	if tag == "" {
		return nil, nil
	}
	data, err := c.request(ctx, "GET", "/Users/"+url.PathEscape(c.Session.UserID)+"/Images/Primary", url.Values{"tag": {tag}, "maxWidth": {"128"}, "maxHeight": {"128"}, "format": {"Png"}}, nil)
	if err != nil {
		return nil, err
	}
	return bitmap.Decode(data, 128, 128)
}
