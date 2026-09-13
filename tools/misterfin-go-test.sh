#!/bin/bash
# Launch from MiSTer's Scripts menu so Main_MiSTer enables framebuffer output.
# The temporary binaries must be prepared again after a reboot.
# Login and playback choices persist on the SD card.
set -eu

# Address the active virtual console directly. Scripts stdout can point at a
# different terminal, leaving the CRT's login text untouched.
clear_console() {
    printf '\033[0m\033[40m\033[2J\033[3J\033[H' > /dev/tty0
}
finish() {
    clear_console
    printf '\033[?25h' > /dev/tty0
}
trap finish EXIT
clear_console
printf '\033[?25l' > /dev/tty0

binary=/tmp/misterfin-go-arm
player=/tmp/misterfin-go-mplayer-arm
if [ ! -x "$binary" ]; then
    echo "Copy the Go ARM build to $binary before running this test."
    exit 1
fi

if [ ! -x "$player" ]; then
    echo "Copy the Go-specific MPlayer build to $player before running this test."
    exit 1
fi

# Main_MiSTer pins Scripts to CPU 1. Let Go and its decoder share both cores.
# The Cortex-A9 decoder cannot keep up while competing with the UI on one core.
taskset -p 3 "$$" >/dev/null

"$binary" -browse \
    -config /media/fat/misterfin/jellyfin.conf \
    -state-dir /media/fat/misterfin-go/state \
    -player "$player"
