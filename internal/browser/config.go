package browser

import (
	"context"
	"misterfin-crt/internal/diagnostics"
	"misterfin-crt/internal/jellyfin"
	"misterfin-crt/internal/remote"

	"misterfin-crt/internal/release"
)

// Config supplies storage paths and release information to [Run]. The caller
// chooses platform defaults. Run does not resolve paths from the display or
// decoder configuration.
type Config struct {
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

	// ConfigPath names the Jellyfin configuration file. Its directory also
	// supplies the default music.json location.
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
