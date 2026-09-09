# Initial Go video playback

The Go client can start and resume movies, episodes, videos, and music videos through Jellyfin's progressive MPEG-2/MP3 transcode endpoint. Desktop playback opens FFplay in a separate window. The MiSTer path launches the existing `mplayer-arm` executable and suspends browser framebuffer writes until the player stops. The C application remains unchanged.

## Desktop use

FFplay must be installed and available as `ffplay` on `PATH`. Run the browser against a real Jellyfin server:

```sh
python3 tools/ghostty/ghostty_harness.py --browse --ntsc --config jellyfin.conf
```

Use `--pal` for PAL. Open a movie's details, then press B or Enter to play. If Jellyfin has an unwatched resume position, playback starts there. A in Ghostty stops playback and returns to details. Any keypress in the FFplay window closes the video window. Q or Ctrl+C in Ghostty stops playback and exits the browser.

Ghostty continues to show the details screen during desktop playback. Video appears in its own window. The inherited mock-server demo supplies browsing data and artwork but does not serve playable videos. Automated decoding tests use a generated video fixture instead.

## Stream and session behavior

The stream query follows `jf_stream_url` in `src/jellyfin.c`: progressive MPEG-2 video in MPEG-TS, stereo MP3 at 48 kHz, no video stream copy, a 720×576 maximum frame, 12 Mbps video, and a 25 or 30 fps cap based on the display mode. Each attempt gets a unique play session ID. Custom transcode profiles remain pending.

Go owns HTTP and TLS. It sends media through an anonymous pipe to the player. Player arguments contain no server URL or token. TLS verification follows the browser configuration, and redirects remain limited to the configured server origin. Player output is consumed only for numeric playback positions. Raw player diagnostics are not printed or logged.

The client reports session start after receiving player position feedback, then sends progress every ten seconds. It also persists the per-user resume position through the C client's user-data endpoint. Stopping cancels the stream, terminates the player process group, reaps the player, and sends a stopped report. A successful exit near the known end of the item marks it watched and clears its resume position. Canceling playback does not newly mark an item watched. Startup failure does not overwrite its resume position. Cleanup reports have a five-second deadline and are best effort if the server is unavailable.

After playback ends, the browser refreshes the details and reuses cached artwork when its image tags are unchanged. Metadata and playback failures return to the browser with an error message.

## MiSTer use and remaining work

MiSTer playback remains blocked by the framebuffer failure. On September 9, 2026, the new ARM build ran its headless test on `192.168.1.42`, but opening the hardware framebuffer returned `open framebuffer: no such device`. The device still runs `6.18.38-MiSTer` and has the existing player. No installed executable was replaced.

The hardware path uses `/media/fat/misterfin/mplayer-arm`. It currently supports 640-pixel-wide PAL and NTSC framebuffers, including doubled 480/576-line output. It uses the source display aspect ratio for letterboxing, ALSA audio, and the existing framebuffer output driver. The direct Go binary accepts `-player` to override the executable path. In headless mode the executable must accept FFplay arguments. On hardware it must accept MPlayer arguments.

Pause, seek, restart selection, subtitles, audio-track selection, music playback, Live TV negotiation, DDR output, HDMI layouts, and hardware timing validation remain pending. The first implementation deliberately covers starting a library video, reporting its session, stopping, and returning to browsing.

## Validation

Go tests cover the C stream query, unique session IDs, fragmented player feedback, invalid positions, CRT aspect calculations, normal completion, startup failure, cancellation, stopped reports, and watched/resume persistence. If FFmpeg and FFplay are installed, a test generates a three-second MPEG-2/MP3 clip and decodes it with FFplay using dummy SDL output. That test validates decoding and position feedback without opening a visible window.

The browser integration test uses a controlled player process and mock HTTP server to exercise details → playback → stop → details → library navigation. Concurrent HTTP handling allows the media connection and API requests to proceed independently.

```sh
make -f Makefile.port test
make -f Makefile.port test-browse
go test -race ./...
go vet ./...
make -f Makefile.port arm
```

Host tests with and without cgo, race checks, `go vet`, 18 Ghostty helper tests, four browser integration tests, and the ARM cross-build passed. Real Jellyfin movie playback and physical CRT playback remain unverified.
