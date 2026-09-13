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

	"misterfin-crt/internal/browser"
	"misterfin-crt/internal/input"
	"misterfin-crt/internal/platform"
	"misterfin-crt/internal/playback"
	"misterfin-crt/internal/videoout"
	"misterfin-crt/internal/videoout/companion"
	"misterfin-crt/internal/videoout/framefile"
	"misterfin-crt/internal/videoout/native"
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

// runBrowser is the composition root: concrete output selection belongs here.
func runBrowser(ctx context.Context, d platform.Display, o launchOptions) (err error) {
	bindings, err := input.LoadConfig(o.inputConfig, o.config)
	if err != nil {
		return err
	}
	if o.stateDir == "" {
		dir, e := os.UserConfigDir()
		if e != nil {
			return e
		}
		o.stateDir = filepath.Join(dir, "misterfin-crt")
	}
	g := d.Geometry()
	var video videoout.Output = companion.New(d)
	if o.terminalPlayer != "" {
		video = framefile.New(d, o.output+".video")
	} else if o.headless == "" {
		video = native.New(d, native.OverlayPath)
	}
	defer func() { err = errors.Join(err, video.Close()) }()
	preferences := playback.NewPreferences(o.stateDir)
	defer func() { err = errors.Join(err, preferences.Close()) }()
	player := playback.Options{
		Preferences: preferences,
		AudioPlayer: o.audioPlayer, Player: o.player, TerminalPlayer: o.terminalPlayer,
		FrameOutput: o.output, Headless: o.headless != "", Device: o.device,
		Width: g.OutputWidth, Height: g.OutputHeight,
	}
	return browser.Run(ctx, o.config, o.stateDir, player, video, browser.NewRenderer(), bindings)

}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
