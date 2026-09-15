package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"misterfin-crt/internal/mister"
	"misterfin-crt/internal/mister/displaymode"
	"misterfin-crt/internal/platform"
)

func run() (err error) {
	o, err := parseOptions(os.Args[1:])
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return err
	}
	source, err := loadSettings(o)
	if err != nil {
		return err
	}
	if o.migrateSettings {
		if err := source.Migrate(); err != nil {
			return err
		}
		fmt.Fprintln(os.Stdout, "Created", source.Path, "(legacy files preserved)")
		return nil
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var mode displaymode.Config
	var loadErr error
	interlaced := os.Getenv(displaymode.ActiveEnv) == "1"
	if o.headless == "" && !interlaced {
		mode, loadErr = displaymode.Parse(source.Section("display"))
	}
	trace, err := openStartupDiagnostics(o, mode.Interlaced && loadErr == nil, source)
	if err != nil {
		return err
	}
	defer func() { trace.close(err) }()
	trace.phase("display-config")
	if loadErr != nil {
		return loadErr
	}
	if o.headless == "" {
		mister.RecordStartup(trace.log, interlaced)
	}
	if mode.Interlaced {
		trace.phase("interlaced-supervisor")
		return displaymode.Run(ctx, filepath.Dir(o.config), os.Args[1:])
	}
	trace.phase("display-open")
	d, err := platform.Open(platform.Options{Device: o.device, Headless: o.headless, Output: o.output})
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, d.Close()) }()
	g := d.Geometry()
	trace.log.Record("application.display", slog.Int("ui_width", g.Width), slog.Int("ui_height", g.Height), slog.Int("output_width", g.OutputWidth), slog.Int("output_height", g.OutputHeight))
	if o.browse {
		return runBrowser(ctx, d, o, trace, source)
	}
	return runPreview(ctx, d, o)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
