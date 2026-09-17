// Package jellyfin provides authenticated access to library metadata, artwork,
// media streams, and playback reporting. Configuration and sessions are shared
// by one Client. Endpoint-specific methods live alongside their query logic.
package jellyfin

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"sync/atomic"
	"time"

	"mistervision/internal/diagnostics"
	"mistervision/internal/media"
)

// Client shares configuration, authentication, and HTTP transport across all
// endpoint methods. After authentication, request methods can run concurrently.
// Callers must serialize Authenticate and any mutation of Config, Session, Version, or HTTP
// against other operations. Returned images and item data belong to the caller.
type Client struct {
	// Diagnostics is optional and borrowed. Set it before starting requests.
	Diagnostics *diagnostics.Log
	// Version is the application build label. Empty reports dev. Set before requests.
	Version string
	Config  Config
	Session Session
	HTTP    *http.Client
	queue   atomic.Pointer[PlaybackQueue]
}

var _ media.Server = (*Client)(nil)

// Identity preserves existing Jellyfin cache and preference keys.
func (c *Client) Identity() media.Identity {
	return media.Identity{Server: c.Config.Server, User: c.Session.UserID}
}

// NewClient copies configuration and session values and creates an HTTP client
// with a 15-second timeout. Redirects must stay on the configured origin and are
// limited to fewer than five hops. InsecureTLS is honored only when configured.
func NewClient(c Config, s Session) *Client {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: c.InsecureTLS}
	// Range streaming borrows this transport without the JSON client's total
	// timeout. Keep its header wait bounded while reusing pooled connections.
	t.ResponseHeaderTimeout = 15 * time.Second
	h := &http.Client{Timeout: 15 * time.Second, Transport: t, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 || req.URL.Scheme != via[0].URL.Scheme || req.URL.Host != via[0].URL.Host {
			return errors.New("redirect outside server refused")
		}
		return nil
	}}
	return &Client{Config: c, Session: s, HTTP: h}
}

// Authorization returns the authenticated client identity for requests to the
// configured Jellyfin server, including WebSocket upgrades. The value contains
// credentials and must not be logged or forwarded to another origin.
func (c *Client) Authorization() string {
	// Quote saved values so they cannot inject authorization fields.
	version := c.Version
	if version == "" {
		version = "dev"
	}
	auth := `MediaBrowser Client="MiSTerVision", Device="MiSTerVision", Version=` + strconv.Quote(version) + `, DeviceId=` + strconv.Quote(c.Session.DeviceID)
	if c.Session.Token != "" {
		auth += ", Token=" + strconv.Quote(c.Session.Token)
	}
	return auth
}

// HTTPError reports a non-success HTTP status without retaining response bodies
// or credential-bearing URLs.
type HTTPError struct{ Status int }

// Error returns a status-only diagnostic suitable for display.
func (e *HTTPError) Error() string { return fmt.Sprintf("Jellyfin returned HTTP %d", e.Status) }

// Is identifies authentication rejection through the shared media error.
func (e *HTTPError) Is(target error) bool {
	return target == media.ErrUnauthorized && (e.Status == 401 || e.Status == 403)
}

// Rejected reports whether err wraps an authentication or authorization failure
// (HTTP 401 or 403). Temporary transport errors do not reject saved credentials.
func Rejected(err error) bool {
	return errors.Is(err, media.ErrUnauthorized)
}

// request sends authenticated JSON or artwork requests and limits buffered
// responses to 8 MiB. It closes each response body and omits request URLs from
// transport errors. Streaming media uses OpenStream instead.
func (c *Client) request(ctx context.Context, method, path string, query url.Values, body any) (data []byte, resultErr error) {
	status := 0
	var received int64
	if c.Diagnostics != nil {
		started := time.Now()
		defer func() { c.Diagnostics.Request(method, path, status, time.Since(started), received, resultErr != nil) }()
	}
	var encoded []byte
	var err error
	if body != nil {
		encoded, err = json.Marshal(body)
		if err != nil {
			return nil, err
		}
	}
	u := c.Config.Server + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, bytes.NewReader(encoded))
	if err != nil {
		return nil, errors.New("invalid request URL")
	}
	req.Header.Set("Authorization", c.Authorization())
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("cannot reach Jellyfin (check address, TLS certificate, and connection)")
	}
	defer resp.Body.Close()
	status = resp.StatusCode
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &HTTPError{resp.StatusCode}
	}
	const limit = 8 << 20
	data, err = io.ReadAll(io.LimitReader(resp.Body, limit+1))
	received = int64(len(data))
	if err != nil {
		return nil, errors.New("cannot read Jellyfin response")
	}
	if len(data) > limit {
		return nil, errors.New("Jellyfin response exceeds 8 MiB")
	}
	return data, nil
}

func (c *Client) json(ctx context.Context, method, path string, q url.Values, body, out any) error {
	b, err := c.request(ctx, method, path, q, body)
	if err != nil {
		return err
	}
	if err = json.Unmarshal(b, out); err != nil {
		return errors.New("invalid Jellyfin JSON response")
	}
	return nil
}
