package main

import (
	"os"
	"path/filepath"

	"misterfin-crt/internal/browser"
	"misterfin-crt/internal/diagnostics"
	"misterfin-crt/internal/settings"
	"misterfin-crt/internal/sound"
)

// browserConfig loads optional browsing settings and resolves storage at startup.
// The cache override retains the C client's root convention while keeping Go
// cache files in their own directory.
func browserConfig(o launchOptions, log *diagnostics.Log, source *settings.File) (browser.Config, error) {
	config := browser.Config{ConfigPath: o.config, StateDir: o.stateDir, Diagnostics: log}
	config.MusicConfig = source.Section("music_visuals")
	if override := os.Getenv("MISTERFIN_MUSIC_CONFIG"); override != "" {
		config.MusicConfig = settings.Read(override, 64<<10, true)
	}
	title, err := browser.ParseTitle(source.Section("ui"))
	if err != nil {
		log.ConfigurationFallback("ui", "default-title", err)
		config.StartupNotices = append(config.StartupNotices, "Could not load title settings. Using MiSTerFin CRT.")
	} else {
		config.Title = title
	}
	background, err := browser.ParseBackground(source.Section("background"))
	if err != nil {
		log.ConfigurationFallback("background", "normal-artwork", err)
		config.StartupNotices = append(config.StartupNotices, "Custom background unavailable. Using normal artwork.")
	} else {
		config.Background = background
	}
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

// browsingSounds uses defaults for an absent optional file. Invalid settings or
// a missing explicit override disable feedback so failure cannot raise its volume.
// The notice is safe to show on screen and contains no raw file contents.
func browsingSounds(o launchOptions, log *diagnostics.Log, source *settings.File) (sound.Config, string) {
	section := source.Section("ui.navigation_sounds")
	if o.soundConfig != "" {
		section = settings.Read(o.soundConfig, 4096, true)
	}
	config, err := sound.ParseConfig(section)
	if err != nil {
		log.ConfigurationFallback("ui.navigation_sounds", "sounds-off", err)
		return sound.Config{}, "Check sound settings. Navigation sounds are off."
	}
	return config, ""
}
