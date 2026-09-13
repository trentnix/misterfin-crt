package main

import (
	"errors"
	"flag"
	"os"
	"time"
)

// launchOptions contains command-line choices before opening any resources.
type launchOptions struct {
	headless, output, device            string
	hold                                time.Duration
	wait, browse                        bool
	player, audioPlayer, terminalPlayer string
	config, stateDir                    string
}

// parseOptions reads arguments without the executable name. Each call uses a
// fresh flag set and current environment defaults. It validates mode combinations
// before opening resources and returns flag.ErrHelp for a help request.
func parseOptions(args []string) (launchOptions, error) {
	var o launchOptions
	flags := flag.NewFlagSet("misterfin-go", flag.ContinueOnError)
	flags.StringVar(&o.headless, "headless", os.Getenv("MISTERFIN_FB"), "headless output geometry, for example 640x288")
	flags.StringVar(&o.output, "output", os.Getenv("MISTERFIN_FRAME_OUT"), "headless BGRX raw output path")
	flags.StringVar(&o.device, "device", "/dev/fb0", "Linux framebuffer device")
	flags.DurationVar(&o.hold, "hold", 0, "keep test frame visible for this duration, for example 10s")
	flags.BoolVar(&o.wait, "wait", false, "keep test frame visible until interrupted")
	flags.StringVar(&o.player, "player", "", "player executable (FFplay for headless preview, mplayer-arm on MiSTer)")
	flags.StringVar(&o.audioPlayer, "audio-player", "", "Python helper for controllable desktop music playback")
	flags.StringVar(&o.terminalPlayer, "terminal-player", "", "Python helper for video in the headless framebuffer")
	flags.BoolVar(&o.browse, "browse", false, "browse Jellyfin with terminal keyboard input")
	flags.StringVar(&o.config, "config", "jellyfin.conf", "Jellyfin configuration path")
	flags.StringVar(&o.stateDir, "state-dir", "", "Go session directory (default: user config directory/misterfin-go)")
	if err := flags.Parse(args); err != nil {
		return o, err
	}
	if flags.NArg() != 0 || o.hold < 0 {
		return o, errors.New("unexpected arguments or negative hold duration")
	}
	if o.wait && o.hold != 0 {
		return o, errors.New("use either -wait or -hold")
	}
	if o.browse && (o.wait || o.hold != 0) {
		return o, errors.New("-browse cannot be combined with -wait or -hold")
	}
	if o.terminalPlayer != "" && (!o.browse || o.headless == "" || o.output == "" || o.player != "") {
		return o, errors.New("-terminal-player requires -browse, -headless, and -output, without -player")
	}
	if o.audioPlayer != "" && (o.headless == "" || !o.browse || o.player != "") {
		return o, errors.New("-audio-player requires headless browsing without -player")
	}
	return o, nil
}
