package plex

import (
	"context"
	"image"
	"io"
	"net/http"
	"net/url"

	"mistervision/internal/artwork/bitmap"
	"mistervision/internal/connection"
)

// homeAvatars contains only public artwork addresses. It never sends account
// headers. Membership and PIN verification do not wait for this service.
type homeAvatars struct {
	client *http.Client
	urls   map[string]string
}

var _ connection.ProfileAvatars = (*homeAvatars)(nil)

// Load decodes a bounded avatar from Plex's public image hosts. The caller owns
// cancellation and concurrency. Missing or unsupported images use the UI fallback.
func (a *homeAvatars) Load(ctx context.Context, id string) (image.Image, error) {
	raw := a.urls[id]
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 4096 || !homeAvatarURL(u) {
		return nil, errHome
	}
	for key := range u.Query() {
		if key != "c" && key != "v" {
			return nil, errHome
		}
	}
	client := *a.client
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 || !homeAvatarURL(req.URL) {
			return errHome
		}
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, "GET", raw, nil)
	if err != nil {
		return nil, errHome
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, errHome
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, (4<<20)+1))
	if err != nil || len(data) > 4<<20 {
		return nil, errHome
	}
	return bitmap.Decode(data, 128, 128)
}

// homeAvatarURL admits Plex's public avatar redirect without forwarding account
// headers or accepting arbitrary origins from profile metadata.
func homeAvatarURL(u *url.URL) bool {
	return u.Scheme == "https" && (u.Host == "plex.tv" || u.Host == "www.plex.tv" || u.Host == "assets.plex.tv") && u.User == nil && u.Fragment == ""
}
