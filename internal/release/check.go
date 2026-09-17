package release

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const latestURL = "https://api.github.com/repos/trentnix/mistervision/releases/latest"

// ErrUnavailable means no public latest release is visible. GitHub also returns
// 404 for private repositories, so callers must not report that as up to date.
var ErrUnavailable = errors.New("no public release available")

// Status describes a published stable release. Available means it is newer than
// the installed stable version, or the installed build has no comparable version.
type Status struct {
	Latest    string
	Available bool
	// Notes is the bounded release summary, or full notes for older releases.
	Notes string
	// HasBundle reports that both the MiSTer ZIP and its checksum asset exist.
	// Installation still verifies the archive and its update protocol.
	HasBundle bool
}

// Check queries this project's latest public stable release with a bounded wait.
// It sends no Jellyfin or GitHub credentials. Context cancellation stops the HTTP
// request. A private repository needs public release metadata before this client
// can detect updates. No response URLs are executed or downloaded.
func Check(ctx context.Context, installed string) (Status, error) {
	client := &http.Client{Timeout: 8 * time.Second}
	return check(ctx, client, latestURL, installed)
}

func check(ctx context.Context, client *http.Client, endpoint, installed string) (Status, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Status{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "MiSTerVision")
	resp, err := client.Do(req)
	if err != nil {
		return Status{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return Status{}, ErrUnavailable
	}
	if resp.StatusCode != http.StatusOK {
		return Status{}, fmt.Errorf("release check: HTTP %d", resp.StatusCode)
	}
	const limit = 1 << 20
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return Status{}, err
	}
	if len(data) > limit {
		return Status{}, errors.New("release metadata is too large")
	}
	var result struct {
		Tag        string `json:"tag_name"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
		Body       string `json:"body"`
		Assets     []struct {
			Name string `json:"name"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return Status{}, err
	}
	if result.Draft || result.Prerelease {
		return Status{}, ErrUnavailable
	}
	if _, ok := stableParts(result.Tag); !ok || len(result.Tag) > 48 {
		return Status{}, errors.New("release tag is not a stable semantic version")
	}
	archive, sums := false, false
	for _, asset := range result.Assets {
		archive = archive || asset.Name == "mistervision-"+result.Tag+"-mister.zip"
		sums = sums || asset.Name == "SHA256SUMS"
	}
	return Status{Latest: result.Tag, Available: newer(result.Tag, installed), Notes: screenNotes(result.Body), HasBundle: archive && sums}, nil
}

// screenNotes selects the "Release summary" Markdown section before
// bounding its length. Unmarked or empty summaries retain legacy full notes.
// Selection belongs to release metadata, independent of display formatting.
func screenNotes(body string) string {
	lines := strings.Split(body, "\n")
	for start, line := range lines {
		if strings.TrimSpace(line) != "## Release summary" {
			continue
		}
		end := start + 1
		for end < len(lines) {
			heading := strings.TrimSpace(lines[end])
			if strings.HasPrefix(heading, "# ") || strings.HasPrefix(heading, "## ") {
				break
			}
			end++
		}
		if summary := strings.TrimSpace(strings.Join(lines[start+1:end], "\n")); summary != "" {
			body = summary
		}
		break
	}
	notes := []rune(body)
	if len(notes) > 8192 {
		notes = append(notes[:8192], []rune("\n[Release notes truncated]")...)
	}
	return string(notes)
}
