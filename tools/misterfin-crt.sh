#!/bin/bash
# Install as /media/fat/Scripts/MiSTerFin-CRT.sh. Main_MiSTer does not quote paths.
# Launch from MiSTer's Scripts menu so Main_MiSTer enables framebuffer output.
# Binaries, login, and playback choices persist on the SD card.
set -eu

# Address the active virtual console directly. Scripts stdout can point at a
# different terminal, leaving the CRT's login text untouched.
clear_console() {
    printf '\033[0m\033[40m\033[2J\033[3J\033[H' > /dev/tty0
}
finish() {
    local status=$?
    if [ "$status" -eq 0 ]; then
        clear_console
    fi
    printf '\033[?25h' > /dev/tty0

    # MiSTer's Scripts wrapper waits for a key after the launcher returns.
    # Reload the menu on success. Leave errors visible for troubleshooting.
    if [ "$status" -eq 0 ] && [ -p /dev/MiSTer_cmd ] && [ -f /media/fat/menu.rbf ]; then
        if ! timeout 2 sh -c 'printf "%s\n" "load_core /media/fat/menu.rbf" > /dev/MiSTer_cmd'; then
            echo "Could not return to the MiSTer menu. Press any key to continue." >&2
        fi
    fi
}
trap finish EXIT
clear_console
printf '\033[?25l' > /dev/tty0

binary=/media/fat/misterfin-crt/misterfin-crt
player=/media/fat/misterfin-crt/mplayer-arm
if [ ! -x "$binary" ]; then
    echo "Install the Go ARM build at $binary before launching MiSTerFin CRT."
    exit 1
fi

if [ ! -x "$player" ]; then
    echo "Install the Go-specific MPlayer build at $player before launching MiSTerFin CRT."
    exit 1
fi

# Main_MiSTer pins Scripts to CPU 1. Let Go and its decoder share both cores.
# The Cortex-A9 decoder cannot keep up while competing with the UI on one core.
taskset -p 3 "$$" >/dev/null

"$binary" -browse \
    -config /media/fat/misterfin-crt/jellyfin.conf \
    -state-dir /media/fat/misterfin-crt/state \
    -player "$player"
