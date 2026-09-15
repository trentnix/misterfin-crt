// Configuration and query behavior are derived from MiSTerFin (CC BY-NC 4.0).

package jellyfin

import (
	"bufio"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
)

// Config holds server credentials, display convention, and video conversion limits.
// LoadConfig validates file-based profiles before authentication or playback.
type Config struct {
	// Transcode sets server-side limits for recorded video and Live TV. Zero uses defaults.
	Transcode                        TranscodeProfile
	Server, APIKey, Username, TVMode string
	InsecureTLS                      bool
	DebugLog                         bool // Enables optional diagnostics unless settings.json diagnostics overrides it.
}

// LoadConfig reads credentials and independent option lines. Invalid transcode
// profiles report a line number without exposing configuration contents.
func LoadConfig(path string) (Config, error) {
	c := Config{TVMode: "PAL", Transcode: DefaultTranscodeProfile()}
	f, err := os.Open(path)
	if err != nil {
		return c, fmt.Errorf("open configuration: %w", err)
	}
	defer f.Close()
	var values []string
	s := bufio.NewScanner(f)
	lineNumber := 0
	for s.Scan() {
		lineNumber++
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if profileLine.MatchString(line) {
			profile, err := parseTranscodeProfile(line, c.Transcode)
			if err != nil {
				return c, fmt.Errorf("invalid transcode profile on line %d: %w", lineNumber, err)
			}
			c.Transcode = profile
			continue
		}
		switch strings.ToUpper(line) {
		case "PAL", "NTSC":
			c.TVMode = strings.ToUpper(line)
		case "INSECURE_TLS":
			c.InsecureTLS = true
		case "DEBUGLOG":
			c.DebugLog = true
		default:
			values = append(values, line)
		}
	}
	if err := s.Err(); err != nil {
		return c, errors.New("cannot read configuration")
	}
	if len(values) < 1 || len(values) > 3 {
		return c, errors.New("configuration needs a server URL, then optional API key and username")
	}
	u, err := url.Parse(values[0])
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return c, errors.New("server must be an HTTP or HTTPS URL without credentials, query, or fragment")
	}
	c.Server = strings.TrimRight(u.String(), "/")
	if len(values) > 1 {
		c.APIKey = values[1]
	}
	if len(values) > 2 {
		c.Username = values[2]
	}
	return c, nil
}
