package jellyfin

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCurrentUserVerifiesTokenIdentity(t *testing.T) {
	for _, body := range []string{`{"Id":"viewer","Name":"Viewer"}`, `{"Id":"other","Name":"Other"}`, `{"Id":"viewer"}`, `not json`} {
		t.Run(body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/Users/Me" || r.URL.RawQuery != "" {
					t.Error("unexpected user request")
				}
				fmt.Fprint(w, body)
			}))
			defer server.Close()
			client := NewClient(Config{Server: server.URL}, Session{UserID: "viewer", Token: "private"})
			user, err := client.CurrentUser(t.Context())
			if body == `{"Id":"viewer","Name":"Viewer"}` {
				if err != nil || user.ID != "viewer" {
					t.Fatal("valid identity rejected")
				}
			} else if err == nil {
				t.Fatal("invalid identity accepted")
			}
		})
	}
}

func TestUserAvatarMissingOrInvalid(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/Users/viewer/Images/Primary" || r.URL.Query().Get("tag") != "avatar-tag" || r.URL.Query().Get("maxWidth") != "128" {
			t.Error("wrong avatar request")
		}
		fmt.Fprint(w, "not an image")
	}))
	defer server.Close()
	client := NewClient(Config{Server: server.URL}, Session{UserID: "viewer", Token: "private"})
	if img, err := client.UserAvatar(t.Context(), ""); img != nil || err != nil || requests != 0 {
		t.Fatal("missing avatar made a request")
	}
	if img, err := client.UserAvatar(t.Context(), "avatar-tag"); img != nil || err == nil || requests != 1 {
		t.Fatal("invalid image accepted")
	}
}
