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
			_, err := client.Authenticate(context.Background(), func(string) { t.Error("unexpected approval code") })
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}

func TestSavedAuthenticationReturnsVerifiedViewer(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/Users/Me" {
			t.Errorf("authentication requested unrelated data: %s", r.URL.Path)
		}
		fmt.Fprint(w, `{"Id":"viewer","Name":"Viewer","PrimaryImageTag":"avatar"}`)
	}))
	defer server.Close()
	saved := Session{Server: server.URL, Token: "saved", UserID: "viewer", DeviceID: "device"}
	client := NewClient(Config{Server: server.URL}, saved)
	user, err := client.Authenticate(t.Context(), func(string) { t.Error("started replacement sign-in") })
	if err != nil || user.ID != "viewer" || user.Name != "Viewer" || user.PrimaryImageTag != "avatar" {
		t.Fatalf("authenticated viewer: %+v, %v", user, err)
	}
	if requests != 1 || client.Session != saved {
		t.Fatal("saved authentication repeated a request or changed credentials")
	}
}
