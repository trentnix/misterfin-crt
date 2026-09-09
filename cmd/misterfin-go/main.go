package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"misterfin-go/internal/browser"
	"misterfin-go/internal/platform"
	"misterfin-go/internal/testframe"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

func run() (err error) {
	headless := flag.String("headless", os.Getenv("MISTERFIN_FB"), "headless output geometry, for example 640x288")
	output := flag.String("output", os.Getenv("MISTERFIN_FRAME_OUT"), "headless BGRX raw output path")
	device := flag.String("device", "/dev/fb0", "Linux framebuffer device")
	hold := flag.Duration("hold", 0, "keep test frame visible for this duration, for example 10s")
	wait := flag.Bool("wait", false, "keep test frame visible until interrupted")
	browse := flag.Bool("browse", false, "browse Jellyfin with terminal keyboard input")
	config := flag.String("config", "jellyfin.conf", "Jellyfin configuration path")
	stateDir := flag.String("state-dir", "", "Go session directory (default: user config directory/misterfin-go)")
	flag.Parse()
	if flag.NArg() != 0 || *hold < 0 {
		return errors.New("unexpected arguments or negative hold duration")
	}
	if *wait && *hold != 0 {
		return errors.New("use either -wait or -hold")
	}
	if *browse && (*wait || *hold != 0) {
		return errors.New("-browse cannot be combined with -wait or -hold")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	d, err := platform.Open(platform.Options{Device: *device, Headless: *headless, Output: *output})
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, d.Close()) }()
	if *browse {
		if *stateDir == "" {
			dir, e := os.UserConfigDir()
			if e != nil {
				return e
			}
			*stateDir = filepath.Join(dir, "misterfin-go")
		}
		return browser.Run(ctx, d, *config, *stateDir)
	}
	if err = testframe.Present(d); err != nil {
		return err
	}
	g := d.Geometry()
	fmt.Printf("Presented BGRX test frame: logical %dx%d, output %dx%d\n", g.Width, g.Height, g.OutputWidth, g.OutputHeight)
	if *wait {
		<-ctx.Done()
	} else if *hold > 0 {
		timer := time.NewTimer(*hold)
		defer timer.Stop()
		select {
		case <-ctx.Done():
		case <-timer.C:
		}
	}
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
