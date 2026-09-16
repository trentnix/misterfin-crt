// Package plex translates Plex Media Server libraries and playback into the
// shared media model. Account sign-in uses plex.tv. Media stays on the explicitly
// configured server, and private tokens never enter player arguments or errors.
package plex

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"misterfin-crt/internal/diagnostics"
	"misterfin-crt/internal/media"
	"misterfin-crt/internal/serverstate"
)

// Config selects the Plex server, TLS policy, and validated conversion limits.
// Zero limits use 720×576 at 12 Mbps. Codec selection stays in the adapter.
type Config struct {
	Server                            string
	InsecureTLS                       bool
	MaxWidth, MaxHeight, VideoBitrate int
}

// Client owns an authenticated Plex connection. Configure and authenticate it
// before sharing it. Request methods are safe for concurrent use.
type Client struct {
	Config      Config
	Session     serverstate.Session
	Version     string
	Diagnostics *diagnostics.Log
	HTTP        *http.Client
	accountHTTP *http.Client
	accountURL  string
}

// NewClient creates separate transports for the media server and Plex sign-in.
// InsecureTLS applies only to the media server. Plex account linking always
// verifies TLS certificates. Tokens stay in request headers.
func NewClient(c Config, s serverstate.Session) *Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: c.InsecureTLS}
	return &Client{Config: c, Session: s, HTTP: &http.Client{Transport: transport, Timeout: 15 * time.Second, CheckRedirect: sameOrigin}, accountHTTP: &http.Client{Timeout: 15 * time.Second, CheckRedirect: sameOrigin}, accountURL: "https://plex.tv"}
}

func sameOrigin(req *http.Request, via []*http.Request) error {
	if len(via) >= 5 || req.URL.Scheme != via[0].URL.Scheme || req.URL.Host != via[0].URL.Host {
		return errors.New("redirect outside server refused")
	}
	return nil
}

// Identity separates Plex caches from Jellyfin and from other Plex accounts.
func (c *Client) Identity() media.Identity {
	return media.Identity{Server: "plex:" + c.Config.Server, User: c.Session.UserID}
}

// HTTPError contains only a status code. Response bodies and URLs remain private.
type HTTPError struct{ Status int }

// Is exposes authentication rejection without leaking provider status types.
func (e *HTTPError) Is(target error) bool {
	return target == media.ErrUnauthorized && (e.Status == 401 || e.Status == 403)
}

// Error returns a credential-free failure description.
func (e *HTTPError) Error() string { return fmt.Sprintf("Plex returned HTTP %d", e.Status) }

// Rejected reports explicit credential rejection, not temporary network errors.
func Rejected(err error) bool {
	var e *HTTPError
	return errors.As(err, &e) && (e.Status == 401 || e.Status == 403)
}

func (c *Client) headers(req *http.Request, token string) {
	// Prepared stream URLs carry the playback identity through the shared stream
	// interface. Plex requires this identity in a header, including timeline calls.
	q := req.URL.Query()
	if session := q.Get("X-Plex-Session-Identifier"); session != "" {
		req.Header.Set("X-Plex-Session-Identifier", session)
		q.Del("X-Plex-Session-Identifier")
		req.URL.RawQuery = q.Encode()
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Plex-Product", "MiSTerFin CRT")
	req.Header.Set("X-Plex-Client-Identifier", c.Session.DeviceID)
	req.Header.Set("X-Plex-Version", c.Version)
	req.Header.Set("X-Plex-Device", "MiSTer")
	req.Header.Set("X-Plex-Platform", "Linux")
	if token != "" {
		req.Header.Set("X-Plex-Token", token)
	}
}

func (c *Client) request(ctx context.Context, method, path string, q url.Values) (data []byte, resultErr error) {
	started := time.Now()
	status := 0
	defer func() {
		c.Diagnostics.Request(method, "/plex-request", status, time.Since(started), int64(len(data)), resultErr != nil)
	}()
	data, status, resultErr = c.fetch(ctx, c.HTTP, c.Config.Server, c.Session.Token, method, path, q)
	return
}

func (c *Client) fetch(ctx context.Context, h *http.Client, origin, token, method, path string, q url.Values) ([]byte, int, error) {
	// All server-provided paths must stay relative to the configured origin.
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || strings.ContainsAny(path, "?#\\") {
		return nil, 0, errors.New("invalid Plex request path")
	}
	raw := origin + path
	if len(q) > 0 {
		raw += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, raw, nil)
	if err != nil {
		return nil, 0, errors.New("invalid Plex request")
	}
	c.headers(req, token)
	response, err := h.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, 0, ctx.Err()
		}
		return nil, 0, errors.New("cannot reach Plex (check address, certificate, and connection)")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, response.StatusCode, &HTTPError{response.StatusCode}
	}
	const limit = 8 << 20
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, response.StatusCode, errors.New("cannot read Plex response")
	}
	if len(data) > limit {
		return nil, response.StatusCode, errors.New("Plex response exceeds 8 MiB")
	}
	return data, response.StatusCode, nil
}

func (c *Client) json(ctx context.Context, path string, q url.Values, out any) error {
	data, err := c.request(ctx, "GET", path, q)
	if err != nil {
		return err
	}
	if json.Unmarshal(data, out) != nil {
		return errors.New("invalid Plex JSON response")
	}
	return nil
}
