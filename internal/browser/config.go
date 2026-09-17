package browser

import (
	"context"
	"image"

	"misterfin-crt/internal/connection"
	"misterfin-crt/internal/diagnostics"
	"misterfin-crt/internal/musicviz"
	"misterfin-crt/internal/release"
	"misterfin-crt/internal/update"
)

// Config supplies browsing settings, storage, and release information to [Run]. The caller
// chooses platform defaults. Run does not resolve paths from the display or
// decoder configuration.
type Config struct {
	// Connector supplies authentication and safe setup instructions for the selected backend.
	Connector connection.Connector

	// MusicVisuals contains validated immutable presets. Nil disables visuals.
	// Startup assembly supplies defaults. Selected images load on a worker.
	MusicVisuals *musicviz.Library

	// Title replaces the heading on the carousel and root library list.
	// Nil uses MiSTerFin CRT. An empty value hides the heading. The renderer
	// truncates it before the clock. The caller must not modify the value during Run.
	Title *string

	// ShowCollections and ShowPlaylists control their home cards. Nil shows a
	// category when nonempty. False hides it even when populated. Callers must
	// not modify these values during Run.
	ShowCollections, ShowPlaylists *bool

	// StartupNotices appear in order once browsing is ready, four seconds each.
	// Use short messages suitable for display. Run copies the slice.
	StartupNotices []string

	// Background is an immutable custom image for carousel and list screens.
	// Nil preserves the default mosaic and item backdrops.
	Background image.Image

	// Diagnostics is borrowed until Run and its tracked cleanup finish. Nil disables logging.
	Diagnostics *diagnostics.Log
	// Build identifies the executable in About and in server client requests.
	Build release.Build
	// CheckUpdate optionally checks release availability. It must honor context
	// cancellation. Nil disables network checks. The browser serializes calls.
	CheckUpdate func(context.Context) (release.Status, error)
	// Updater installs a release outside the event loop. Nil permits release
	// notes but requires manual installation. Run cancels and joins active work.
	Updater update.Installer
	// RestartAfterUpdate lets Run return update.ErrRestart after installation.
	// Enable only when the caller and launcher support restarting after cleanup.
	RestartAfterUpdate bool

	// StateDir holds private authentication and playback state.
	StateDir string
	// ArtworkCacheDir holds decoded artwork. Empty disables artwork disk caching.
	// The browser adds a subdirectory for each server and user after sign-in.
	ArtworkCacheDir string
	// MosaicCacheDir holds library background collages. Empty disables mosaic
	// disk caching. The browser adds the same server and user isolation as artwork.
	MosaicCacheDir string
}
