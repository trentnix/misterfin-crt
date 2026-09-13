package browser

// Config supplies resolved storage paths to [Run]. The caller chooses platform
// defaults. Run does not resolve paths from the display or decoder configuration.
type Config struct {
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
