package jellyfin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestVerifyServerRefusesInvalidIdentityAndRedirects(t *testing.T) {
	for _, response := range []string{"match", "different", "missing", "malformed", "oversized", "redirect", "http-error"} {
		t.Run(response, func(t *testing.T) {
			redirected := false
			destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected = true }))
			defer destination.Close()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "" || r.URL.Path != "/System/Info/Public" {
					t.Error("invalid identity probe")
				}
				switch response {
				case "match":
					fmt.Fprint(w, `{"Id":"saved-id"}`)
				case "different":
					fmt.Fprint(w, `{"Id":"other-id"}`)
				case "missing":
					fmt.Fprint(w, `{}`)
				case "malformed":
					fmt.Fprint(w, `{`)
				case "oversized":
					fmt.Fprint(w, strings.Repeat(" ", 65537))
				case "redirect":
					http.Redirect(w, r, destination.URL, http.StatusFound)
				case "http-error":
					w.WriteHeader(503)
				}
			}))
			defer server.Close()
			err := VerifyServer(t.Context(), server.URL, "saved-id")
			if (err == nil) != (response == "match") || redirected {
				t.Fatalf("identity result: %v, redirected: %v", err, redirected)
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := VerifyServer(ctx, "http://127.0.0.1:1", "saved-id"); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation was lost")
	}
}
