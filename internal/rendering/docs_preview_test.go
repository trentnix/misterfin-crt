package rendering

import (
	"os"
	"testing"
	"time"

	"mistervision/internal/branding"
	"mistervision/internal/connection"
	"mistervision/internal/release"
	"mistervision/internal/ui"
)

// TestDocumentationPreviews exports setup examples with the production renderer.
// It is opt-in so normal tests never overwrite documentation. The identities,
// addresses, approval codes, and avatar are fixtures, not saved account data.
func TestDocumentationPreviews(t *testing.T) {
	dir := os.Getenv("DOCS_PREVIEW_DIR")
	if dir == "" {
		t.Skip("set DOCS_PREVIEW_DIR to export documentation previews")
	}
	choices := []connection.Choice{
		{ID: "existing", Name: "Use existing connection", Description: "Choose a configured or remembered server", Children: []connection.Choice{{ID: "home", Name: "Home Jellyfin"}}},
		{ID: "jellyfin", Name: "Jellyfin", Description: "Find a server on your local network"},
		{ID: "plex", Name: "Plex", Description: "Link your account and choose a server"},
	}
	profiles := []connection.Profile{
		{ID: "alex", Name: "Alex", Protected: true, Avatar: branding.Logo()},
		{ID: "sam", Name: "Sam"},
		{ID: "guest", Name: "Guest"},
		{ID: "taylor", Name: "Taylor"},
	}
	scenes := map[string]Scene{
		"connections": {About: AboutPresentation{Visible: true, ConnectionsVisible: true, Connections: choices, ConnectionSelected: 2}},
		"jellyfin-discovery": {Setup: SetupPresentation{
			Kind: SetupServers, Title: "Choose a server",
			Servers: []connection.Server{{ID: "home", Name: "Home Jellyfin", URL: "http://192.0.2.10:8096"}},
		}},
		"quick-connect": {Setup: SetupPresentation{
			Kind: SetupApproval, Title: "Quick Connect", Retry: "New code", Code: "123456", BackToServers: true,
			Message: "In a signed-in Jellyfin client, open Quick Connect.\nEnter this code to approve MiSTerVision.",
		}},
		"plex-link": {Setup: SetupPresentation{
			Kind: SetupApproval, Title: "Link Plex", Retry: "New code", Code: "ABCD", BackToServers: true,
			Message: "Open plex.tv/link with the account you want to use.\nEnter this code to approve MiSTerVision.",
		}},
		"plex-servers": {Setup: SetupPresentation{
			Kind: SetupServers, Title: "Choose a Plex server", Message: "Signed in as Alex.",
			Servers: []connection.Server{{ID: "home", Name: "Home Plex", URL: "http://192.0.2.20:32400"}},
			SignIn:  "Sign in with another account", BackToProfiles: true,
		}},
		"plex-profiles": {Setup: SetupPresentation{Kind: SetupProfiles, Profiles: profiles}},
		"plex-pin":      {Setup: SetupPresentation{Kind: SetupPIN, Profiles: profiles, PINLength: 2, PINKey: 4}},
		"setup-help": {Setup: SetupPresentation{
			Kind: SetupFailure, Title: "No Jellyfin servers found", Retry: "Retry",
			Message:   "Check Jellyfin and your local network, then retry.\nOr set server.url in settings.json.",
			PathLabel: "Configuration file", Path: "/media/fat/mistervision/settings.json",
		}},
		"about": {About: AboutPresentation{
			Visible: true, Build: release.Build{Version: "dev"}, Checking: true,
			Profile: &profiles[0], SwitchProfile: true, Connections: choices,
		}},
	}
	for name, scene := range scenes {
		t.Run(name, func(t *testing.T) {
			scene.Now = time.Date(2026, 9, 17, 20, 0, 0, 0, time.UTC)
			if len(scene.About.Connections) == 0 {
				scene.About.Connections = choices
			}
			renderer := NewRenderer()
			frame := renderer.Render(640, 240, scene)
			canvas := ui.New(640, 240)
			copy(canvas.Pixels, frame.UI)
			writeSetupPreview(t, dir, name+".png", canvas)
		})
	}
}
