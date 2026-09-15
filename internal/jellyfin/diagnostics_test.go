package jellyfin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"misterfin-crt/internal/diagnostics"
)

func TestRequestDiagnosticsKeepAuthenticationPrivate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/failure" {
			w.WriteHeader(503)
		}
		fmt.Fprint(w, "response-secret")
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "debug.log")
	log, err := diagnostics.Open(diagnostics.Config{Enabled: true, Path: path, MaxBytes: 8192})
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	c := NewClient(Config{Server: server.URL}, Session{Token: "auth-secret", DeviceID: "device-secret"})
	c.Diagnostics = log
	_, err = c.request(context.Background(), "POST", "/QuickConnect/Connect", url.Values{"secret": {"query-secret"}}, map[string]string{"password": "body-secret"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.request(context.Background(), "GET", "/failure", nil, nil); err == nil {
		t.Fatal("missing failure")
	}
	stream, err := c.OpenStream(context.Background(), server.URL+"/video-secret?ApiKey=stream-secret")
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, stream)
	stream.Close()
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	for _, secret := range []string{"response-secret", "auth-secret", "device-secret", "query-secret", "body-secret", "video-secret", "stream-secret", server.URL} {
		if strings.Contains(string(data), secret) {
			t.Fatalf("leaked %s", secret)
		}
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 3 {
		t.Fatal(string(data))
	}
	var event struct {
		Status int
		Bytes  int
		Failed bool
	}
	if err := json.Unmarshal([]byte(lines[0]), &event); err != nil {
		t.Fatal(err)
	}
	if event.Status != 200 || event.Bytes != len("response-secret") || event.Failed {
		t.Fatal(event)
	}
	if err := json.Unmarshal([]byte(lines[1]), &event); err != nil {
		t.Fatal(err)
	}
	if event.Status != 503 || !event.Failed {
		t.Fatal(event)
	}
}

func TestLegacyDebugLogIsParsed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jellyfin.conf")
	if err := os.WriteFile(path, []byte("http://example.test\nDEBUGLOG\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := LoadConfig(path)
	if err != nil || !c.DebugLog {
		t.Fatalf("%+v %v", c, err)
	}
}
