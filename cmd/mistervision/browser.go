package main

import (
	"context"
	"errors"
	"log/slog"
	"os"

	"mistervision/internal/browser"
	"mistervision/internal/connection"
	"mistervision/internal/input"
	"mistervision/internal/platform"
	"mistervision/internal/playback"
	"mistervision/internal/release"
	"mistervision/internal/rendering"
	"mistervision/internal/settings"
	"mistervision/internal/sound"
)

// runBrowser owns input, preferences, and video output around the shared browser.
// Target assembly supplies independent dependencies before any reader starts.
func runBrowser(ctx context.Context, d platform.Display, o launchOptions, trace *startupDiagnostics, source *settings.File) (err error) {
	trace.phase("input-config")
	inputSettings := source.Section("input")
	if o.inputConfig != "" {
		inputSettings = settings.Read(o.inputConfig, 64<<10, true)
	}
	bindings, err := input.ParseConfig(inputSettings)
	if err != nil {
		return err
	}
	trace.phase("browser-config")
	config, err := browserConfig(o, trace.log, source)
	if err != nil {
		return err
	}
	config.Build = release.CurrentBuild()
	catalog, err := newConnectionCatalog(source, o.config, config.StateDir, config.Build.Version, trace.log)
	if err != nil {
		return err
	}
	config.Connections = catalog.choices
	if catalog.notice != "" {
		config.StartupNotices = append(config.StartupNotices, catalog.notice)
	}
	if executable, err := os.Executable(); err == nil {
		if installer := installedUpdater(o, executable); installer != nil {
			config.Updater = installer
			config.RestartAfterUpdate = os.Getenv("MISTERVISION_AUTO_RESTART") == "1"
		}
	}
	if os.Getenv("MISTERVISION_UPDATE_RECOVERED") == "1" {
		_ = os.Unsetenv("MISTERVISION_UPDATE_RECOVERED")
		trace.log.Record("update.recovered")
		config.StartupNotices = append(config.StartupNotices, "An interrupted update was rolled back.")
	}
	config.CheckUpdate = func(ctx context.Context) (release.Status, error) {
		return release.Check(ctx, config.Build.Version)
	}
	if trace.notice != "" {
		config.StartupNotices = append(config.StartupNotices, trace.notice)
	}
	trace.phase("sound-config")
	soundConfig, notice := browsingSounds(o, trace.log, source)
	if notice != "" {
		config.StartupNotices = append(config.StartupNotices, notice)
	}
	target := selectBrowserTarget(d, o, bindings)
	config.InitialControls = target.initialControls
	if target.activate != nil {
		restore := target.activate()
		defer restore()
	}
	video, player := target.output, target.player
	defer func() { err = errors.Join(err, video.Close()) }()
	trace.phase("sound-open")
	sounds, err := sound.New(soundConfig, target.openSound)
	if err != nil {
		trace.log.ConfigurationFallback("ui.navigation_sounds", "sounds-off", err)
		config.StartupNotices = append(config.StartupNotices, "Navigation sounds unavailable. Continuing without feedback.")
	}
	defer sounds.Close()
	var feedback sound.Feedback
	if sounds != nil {
		feedback = sounds
	}
	preferences := playback.NewPreferences(config.StateDir, trace.log)
	defer func() { err = errors.Join(err, preferences.Close()) }()
	player.Preferences = preferences
	player.Diagnostics = trace.log

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	trace.phase("input-open")
	trace.log.Record("input.backend", slog.Bool("terminal", o.headless != ""), slog.Int("configured_profiles", len(bindings.Profiles)))
	keys, done, err := target.readInput(ctx, trace.log)
	if err != nil {
		video.Clear()
		return err
	}
	defer func() { cancel(); <-done }()
	trace.phase("browser")
	navigation := make(map[string]*browser.Navigation)
	for {
		id := catalog.connectionID(catalog.selected)
		if navigation[id] == nil {
			navigation[id] = &browser.Navigation{}
		}
		config.ConnectionID = id
		config.Navigation = navigation[id]
		config.Connector = catalog.connectors[catalog.selected]
		err := browser.Run(ctx, config, player, video, rendering.NewRenderer(), feedback, keys)
		var change *connection.Change
		if !errors.As(err, &change) {
			return err
		}
		if _, ok := catalog.connectors[change.ID]; !ok {
			return errors.New("unknown connection selection")
		}
		catalog.startSelection(change.ID)
		config.ReturnConnectionID = change.ReturnID
		catalog.selected = change.ID
		if change.SelectProfile {
			catalog.selected = catalog.startProfileSelection(change.ID)
		}
		config.StartupNotices = nil
	}
}
