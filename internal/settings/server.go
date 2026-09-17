package settings

import (
	"errors"
	"net/url"
	"strings"
)

// Server selects one media provider and its connection policy. An absent server
// section selects legacy Jellyfin configuration. Saved sign-in stays separate.
type Server struct {
	Provider    string         `json:"provider"`
	URL         string         `json:"url"`
	InsecureTLS bool           `json:"insecure_tls"`
	Transcode   Transcode      `json:"transcode"`
	Jellyfin    *JellyfinLogin `json:"jellyfin,omitempty"`
}

// Transcode bounds server-side video conversion. Each adapter chooses codecs
// and frame-rate limits for its protocol. Dimensions are limits, not an aspect ratio.
type Transcode struct {
	MaxWidth     int `json:"max_width"`
	MaxHeight    int `json:"max_height"`
	VideoBitrate int `json:"video_bitrate"`
}

// JellyfinLogin supplies optional API-key authentication instead of Quick Connect.
// These private values must not appear in logs or setup errors.
type JellyfinLogin struct {
	APIKey   string `json:"api_key"`
	Username string `json:"username"`
}

// ParseServer validates shared connection settings and returns nil for an absent
// section. Errors describe fields without exposing their values. An invalid
// explicit section never falls back to another server or to legacy credentials.
func ParseServer(section Section) (*Server, error) {
	if section.Data == nil && section.Err == nil {
		return nil, nil
	}
	c := Server{Provider: "jellyfin", Transcode: Transcode{MaxWidth: 720, MaxHeight: 576, VideoBitrate: 12000000}}
	if err := section.Decode(&c); err != nil {
		return nil, errors.New("invalid server settings: check field names and value types")
	}
	if c.Provider != "jellyfin" && c.Provider != "plex" {
		return nil, errors.New("server.provider must be jellyfin or plex")
	}
	u, err := url.Parse(strings.TrimSpace(c.URL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" {
		return nil, errors.New("server.url must be an HTTP or HTTPS address without credentials, query, or fragment")
	}
	c.URL = strings.TrimRight(u.String(), "/")
	if c.Transcode.MaxWidth < 160 || c.Transcode.MaxWidth > 1920 {
		return nil, errors.New("server.transcode.max_width must be 160–1920")
	}
	if c.Transcode.MaxHeight < 120 || c.Transcode.MaxHeight > 1080 {
		return nil, errors.New("server.transcode.max_height must be 120–1080")
	}
	if c.Transcode.VideoBitrate < 100000 || c.Transcode.VideoBitrate > 50000000 {
		return nil, errors.New("server.transcode.video_bitrate must be 100000–50000000 bits per second")
	}
	if c.Jellyfin != nil {
		if c.Provider != "jellyfin" {
			return nil, errors.New("server.jellyfin requires the jellyfin provider")
		}
		if (c.Jellyfin.APIKey == "") != (c.Jellyfin.Username == "") {
			return nil, errors.New("server.jellyfin requires both api_key and username")
		}
	}
	return &c, nil
}
