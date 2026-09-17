package release

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestReleaseComparison(t *testing.T) {
	for _, tc := range []struct {
		latest, installed string
		want              bool
	}{
		{"v1.10.0", "v1.9.0", true}, {"v1.9.0", "v1.10.0", false},
		{"v2.0.0", "v1.99.99", true}, {"v1.2.3", "1.2.3", false},
		{"v1.2.3", "v1.2.3+build.4", false}, {"v1.2.3", "v1.2.3-rc.1", true},
		{"v1.2.3", "v2.0.0-rc.1", false}, {"v1.2.3", "dev", true},
		{"v01.2.3", "dev", false}, {"v1.2", "dev", false}, {"v1.2.3-rc.1", "dev", false},
	} {
		if got := newer(tc.latest, tc.installed); got != tc.want {
			t.Errorf("%s versus %s: %v", tc.latest, tc.installed, got)
		}
	}
}

func TestLatestReleaseResponses(t *testing.T) {
	for _, tc := range []struct {
		name                         string
		code                         int
		body                         string
		available, fail, unavailable bool
	}{
		{"newer", 200, `{"tag_name":"v1.10.0"}`, true, false, false},
		{"current", 200, `{"tag_name":"v1.9.0"}`, false, false, false},
		{"older", 200, `{"tag_name":"v1.8.0"}`, false, false, false},
		{"private or missing", 404, `{}`, false, true, true},
		{"rate limit", 403, `{}`, false, true, false},
		{"server error", 503, `{}`, false, true, false},
		{"malformed", 200, `{`, false, true, false},
		{"null", 200, `null`, false, true, false},
		{"invalid version", 200, `{"tag_name":"hello"}`, false, true, false},
		{"draft", 200, `{"tag_name":"v2.0.0","draft":true}`, false, true, true},
		{"prerelease", 200, `{"tag_name":"v2.0.0","prerelease":true}`, false, true, true},
		{"oversize", 200, strings.Repeat(" ", (1<<20)+1), false, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.Header.Get("Authorization") != "" || r.URL.RawQuery != "" {
					t.Error("unexpected request or credentials")
				}
				if r.Header.Get("User-Agent") != "MiSTerVision" {
					t.Error("missing user agent")
				}
				w.WriteHeader(tc.code)
				w.Write([]byte(tc.body))
			}))
			defer server.Close()
			got, err := check(context.Background(), server.Client(), server.URL, "v1.9.0")
			if (err != nil) != tc.fail || errors.Is(err, ErrUnavailable) != tc.unavailable || got.Available != tc.available {
				t.Fatalf("got %+v, %v", got, err)
			}
		})
	}
}

// Cancellation must interrupt a request already waiting for release metadata.
func TestCheckCancellation(t *testing.T) {
	entered := make(chan struct{})
	stopped := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		select {
		case <-r.Context().Done():
		case <-release:
		}
		close(stopped)
	}))
	defer server.Close()
	defer close(release)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := check(ctx, server.Client(), server.URL, "dev")
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("release request did not reach the server")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancellation did not stop the release check")
	}
	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("cancellation left the HTTP request open")
	}
}

func TestBuildLabel(t *testing.T) {
	b := Build{Version: "dev", Revision: "1234567890", Modified: true}
	if b.String() != "dev (1234567) modified" {
		t.Fatal(b.String())
	}
	if (Build{}).String() != "dev" {
		t.Fatal("empty version should identify development build")
	}
}

func TestReleaseIncludesBoundedNotesAndMatchingAssets(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"tag_name":"v0.2.0","body":%q,"assets":[{"name":"mistervision-v0.2.0-mister.zip"},{"name":"SHA256SUMS"}]}`, strings.Repeat("x", 9000))
	}))
	defer server.Close()
	status, err := check(context.Background(), server.Client(), server.URL, "v0.1.0")
	if err != nil || !status.HasBundle || !status.Available || len(status.Notes) > 8300 || !strings.HasSuffix(status.Notes, "[Release notes truncated]") {
		t.Fatalf("status: %+v, %v", status, err)
	}
}

func TestReleaseSummarySelection(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{"legacy", "Changes without a summary section.", "Changes without a summary section."},
		{"summary only", "## Release summary\n\nAutomatic updates.\n", "Automatic updates."},
		{"exclude instructions", "GitHub introduction.\n\n## Release summary\n\n### Automatic updates\n\nKeep your settings.\n\n## Installation\n\nManual instructions.", "### Automatic updates\n\nKeep your settings."},
		{"top-level boundary", "## Release summary\nUse the updater.\n# Downloads\nGitHub downloads.", "Use the updater."},
		{"windows newlines", "## Release summary\r\n\r\nReopen MiSTerVision.\r\n\r\n## Installation\r\nOther instructions.", "Reopen MiSTerVision."},
		{"empty summary", "## Release summary\n\n## Installation\nManual instructions.", "## Release summary\n\n## Installation\nManual instructions."},
		{"selection before limit", strings.Repeat("x", 9000) + "\n## Release summary\nUseful summary.", "Useful summary."},
		{"bounded Unicode", "## Release summary\n" + strings.Repeat("é", 9000) + "\n## Installation\nDo not show.", strings.Repeat("é", 8192) + "\n[Release notes truncated]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": "v0.2.0", "body": tc.body})
			}))
			defer server.Close()
			status, err := check(context.Background(), server.Client(), server.URL, "v0.1.0")
			if err != nil || !status.Available || status.Notes != tc.want {
				t.Fatalf("notes: %q, available %v, error %v", status.Notes, status.Available, err)
			}
		})
	}
}
