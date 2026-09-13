package main

import (
	"os"
	"path/filepath"

	"misterfin-crt/internal/browser"
)

// browserConfig resolves storage defaults at startup. The cache override retains
// the C client's root convention while keeping Go cache files in their own directory.
func browserConfig(o launchOptions) (browser.Config, error) {
	config := browser.Config{ConfigPath: o.config, StateDir: o.stateDir}
	if config.StateDir == "" {
		dir, err := os.UserConfigDir()
		if err != nil {
			return config, err
		}
		config.StateDir = filepath.Join(dir, "misterfin-crt")
	}
	root := os.Getenv("MISTERFIN_CACHE_ROOT")
	if root == "" {
		if o.headless == "" {
			root = "/media/fat"
		} else {
			// An unavailable user cache directory disables disk caching, as before.
			root, _ = os.UserCacheDir()
		}
	}
	if root != "" {
		config.ArtworkCacheDir = filepath.Join(root, "misterfin-crt", "covercache")
		config.MosaicCacheDir = filepath.Join(root, "misterfin-crt", "gridcache")
	}
	return config, nil
}
