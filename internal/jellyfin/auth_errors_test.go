package jellyfin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthenticationReportsRecoverableConditions(t *testing.T) {
	for _, tc := range []struct {
		name, path, payload, key string
		want                     error
	}{
		{"disabled", "/QuickConnect/Enabled", "false", "", ErrQuickConnectDisabled},
		{"unknown user", "/Users", "[]", "test-key", ErrUsernameNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != tc.path {
					t.Errorf("unexpected request %s", r.URL.Path)
					w.WriteHeader(404)
					return
				}
				fmt.Fprint(w, tc.payload)
			}))
			defer server.Close()
			client := NewClient(Config{Server: server.URL, APIKey: tc.key, Username: "viewer"}, Session{DeviceID: "test"})
			err := client.Authenticate(context.Background(), t.TempDir(), func(string) { t.Error("unexpected approval code") })
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}
