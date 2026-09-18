package jellyfin

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"time"

	"mistervision/internal/media"
)

// OpenStream consumes a prepared video URL without a body-size or total-time limit.
// Video startup allows 60 seconds for headers and uses a dedicated connection.
func (c *Client) OpenStream(ctx context.Context, streamURL string) (body io.ReadCloser, resultErr error) {
	status := 0
	if c.Diagnostics != nil {
		started := time.Now()
		defer func() {
			c.Diagnostics.Request("GET", "/media-stream", status, time.Since(started), 0, resultErr != nil)
		}()
	}
	req, err := c.newStreamRequest(ctx, "GET", streamURL, nil)
	if err != nil {
		return nil, err
	}
	transport := c.HTTP.Transport
	if t, ok := transport.(*http.Transport); ok {
		clone := t.Clone()
		// A seek can start a second expensive HDR transcode while the old
		// stream remains open. Allow its first response to arrive before
		// abandoning the replacement. Context cancellation still stops it.
		clone.ResponseHeaderTimeout = 60 * time.Second
		clone.DisableKeepAlives = true
		transport = clone
	}
	response, err := c.doStream(req, transport)
	if err != nil {
		return nil, err
	}
	status = response.StatusCode
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		response.Body.Close()
		return nil, &HTTPError{Status: response.StatusCode}
	}
	return response.Body, nil
}

// RequestStream opens an audio GET or HEAD request, preserving HTTP status and
// range headers for the local proxy. It reuses the client's pooled transport
// with its bounded header wait and no total timeout for the response body.
func (c *Client) RequestStream(ctx context.Context, method, raw string, headers http.Header) (*http.Response, error) {
	req, err := c.newStreamRequest(ctx, method, raw, headers)
	if err != nil {
		return nil, err
	}
	return c.doStream(req, c.HTTP.Transport)
}

// newStreamRequest accepts only same-origin GET/HEAD URLs. Stream URLs already
// carry server credentials. Only range headers cross the local proxy boundary.
func (c *Client) newStreamRequest(ctx context.Context, method, raw string, headers http.Header) (*http.Request, error) {
	target, err := url.Parse(raw)
	origin, originErr := url.Parse(c.Config.Server)
	if err != nil || originErr != nil || target.Host == "" ||
		(target.Scheme != "http" && target.Scheme != "https") ||
		target.Scheme != origin.Scheme || target.Host != origin.Host || target.User != nil || target.Fragment != "" ||
		(method != "GET" && method != "HEAD") {
		return nil, errors.New("invalid media request")
	}
	req, err := http.NewRequestWithContext(ctx, method, raw, nil)
	if err != nil {
		return nil, errors.New("invalid media request")
	}
	req.Header.Set("Accept-Encoding", "identity")
	for _, name := range []string{"Range", "If-Range"} {
		if value := headers.Get(name); value != "" {
			req.Header.Set(name, value)
		}
	}
	return req, nil
}

// doStream applies the client's redirect policy without its JSON timeout.
// Transport errors must not expose the authenticated URL.
func (c *Client) doStream(req *http.Request, transport http.RoundTripper) (*http.Response, error) {
	client := &http.Client{Transport: transport, CheckRedirect: c.HTTP.CheckRedirect}
	response, err := client.Do(req)
	if err != nil {
		if req.Context().Err() != nil {
			return nil, req.Context().Err()
		}
		return nil, media.NetworkError(err)
	}
	return response, nil
}
