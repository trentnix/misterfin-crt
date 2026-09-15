package browser

import (
	"context"
	"image"
	"misterfin-crt/internal/diagnostics"
	"misterfin-crt/internal/jellyfin"
	"misterfin-crt/internal/remote"
	"misterfin-crt/internal/settings"

	"misterfin-crt/internal/release"
)

// Config supplies browsing settings, storage, and release information to [Run]. The caller
// chooses platform defaults. Run does not resolve paths from the display or
// decoder configuration.
type Config struct {
	// MusicConfig is the immutable music_visuals section snapshot. The session validates
	// it on a worker before loading selected assets. Its zero value uses defaults.
	MusicConfig settings.Section

	// Title replaces the heading on the carousel and root library list.
	// Nil uses MiSTerFin CRT. An empty value hides the heading. The renderer
	// truncates it before the clock. The caller must not modify the value during Run.
	Title *string

	// StartupNotices appear in order once browsing is ready, four seconds each.
	// Use short messages suitable for display. Run copies the slice.
	StartupNotices []string

	// Background is an immutable custom image for carousel and list screens.
	// Nil preserves the default mosaic and item backdrops.
	Background image.Image

	// Remote constructs a control source after sign-in. Nil disables remote control.
	// The browser owns its cancellation and waits for Run before closing.
	Remote func(*jellyfin.Client) remote.Source

	// Diagnostics is borrowed until Run and its tracked cleanup finish. Nil disables logging.
	Diagnostics *diagnostics.Log
	// Build identifies the installed executable on the About page.
	Build release.Build
	// CheckUpdate optionally checks release availability. It must honor context
	// cancellation. Nil disables network checks. The browser serializes calls.
	CheckUpdate func(context.Context) (release.Status, error)

	// ConfigPath names the Jellyfin connection configuration file.
	ConfigPath string
	// StateDir holds the persisted Jellyfin authentication session.
	StateDir string
	// ArtworkCacheDir holds decoded artwork. Empty disables artwork disk caching.
	// The browser adds a subdirectory for each server and user after sign-in.
	ArtworkCacheDir string
	// MosaicCacheDir holds library background collages. Empty disables mosaic
	// disk caching. The browser adds the same server and user isolation as artwork.
	MosaicCacheDir string
}
