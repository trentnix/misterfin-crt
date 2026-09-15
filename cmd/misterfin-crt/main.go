package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if o.headless == "" && os.Getenv(displaymode.ActiveEnv) != "1" {
		mode, loadErr := displaymode.Load(filepath.Join(filepath.Dir(o.config), "display.json"))
		if loadErr != nil {
			return loadErr
		}
		if mode.Interlaced {
			return displaymode.Run(ctx, filepath.Dir(o.config), os.Args[1:])
		}
	}
	d, err := platform.Open(platform.Options{Device: o.device, Headless: o.headless, Output: o.output})
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, d.Close()) }()
	if o.browse {
		return runBrowser(ctx, d, o)
	}
	return runPreview(ctx, d, o)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
