# Media playback

MiSTer uses the patched MPlayer. Local development uses Python/libmpv inside Ghostty. FFplay is an alternate test player with a separate video window. All paths share browsing and playback state. Server adapters own reporting.

Jellyfin and [Plex](GO_PLEX.md) share playback controls and output paths. Each adapter handles stream preparation, progress reporting, and tuner ownership for its server.

If the player reports no playback position within 30 seconds, playback stops and a wrapped “Playback didn't start” message offers retry guidance for eight seconds. Starting playback clears the message. Diagnostics retain the `playback.startup-timeout` event. The timeout does not identify the cause of a slow start. Other failures distinguish a stream that could not start from interrupted playback. A routine progress-reporting failure shows a warning without stopping album or playlist advancement. The warning stays visible across track changes until its eight-second deadline. Subtitle and track-navigation failures offer retry guidance without exposing provider errors.

The browser’s `playbackQueue` owns local and remote queue entries and decoder handoff. Remote command handling and queue publication remain in `remote_commands.go` and `remote_source.go`. Paged playlists and whole-library shuffle retain their separate navigation state.

## Players and local use

| Player | Current use | Limits |
| --- | --- | --- |
| MPlayer | Everyday MiSTer playback with shared CRT overlays. | Requires the matching patched ARM build. |
| Python/libmpv | Inline Ghostty video and controllable desktop music. | Requires Python 3 and libmpv. Terminal uploads can limit smoothness. |
| FFplay | Alternate desktop video and automated decoding tests. | No shared overlay in its video window, no live picture changes, no audio seeking or meter feedback. |

For video inside Ghostty:

```sh
python3 tools/ghostty/video_player.py --check
python3 tools/ghostty/ghostty_harness.py --browse --ntsc --inline-video --settings settings.json
```

Without `--inline-video`, the harness uses FFplay for video. Keep keyboard focus in Ghostty for client controls. The harness uses the Python/libmpv helper for music in either video mode. Direct headless runs without an audio helper use FFplay for music too. MiSTer needs neither Python nor libmpv. See the [harness guide](../tools/ghostty/README.md) and [MPlayer build](GO_BUILD.md#mplayer).

MiSTer's MPlayer retains an 8 MiB read-ahead cache. Recorded video prefills 20% before decoding. Live TV begins demuxing without a cache prefill because a low-bitrate broadcast may not supply 1.6 MiB before the 30-second startup deadline. The frame timer starts when the first decoded image is ready, so opening and buffering do not cause a catch-up burst after loading or seeking.

## Playback controls

| Action | Xbox controller | Keyboard |
| --- | --- | --- |
| Show/hide controls | Any D-pad or left-stick direction | Any arrow |
| Seek backward/forward | LT / RT | J / L |
| Previous/next music track | LB / RB | [ / ] or Page Up / Page Down |
| Pause/resume | B | Enter or B |
| Stop and return | A | Escape or A |
| Video options / music background | Select | Tab |

[Input profiles](GO_INPUT.md) control hardware bindings and badge labels. Controls expire after three seconds. Pause/resume hides them. Menu toggles and track changes act once per press.

Changing items with Next or Previous starts playback even if the previous item was paused. An explicit Pause after requesting the change is retained while the next item loads.

Held seeks repeat after 350 ms, then every 250 ms. Video steps are 30 seconds and music steps are 10 seconds. Live TV does not seek. Shoulders have no action during video.

On an unwatched resumable video's details screen, Open resumes and SELECT/Tab restarts from zero. Restart is a selection-screen action, not an in-playback control. [Photos](GO_BROWSING.md#photos) retain Left/Right navigation and Up for controls. See [music](GO_MUSIC.md) for album queues and shuffle.

## Video seeking

Seek presses accumulate toward a destination. With controls hidden, two quick presses reveal the destination overlay. With controls open, the destination stays in that menu. After 0.5 seconds without another press, the current decoder pauses and a replacement stream is prepared. A new seek during preparation cancels that replacement and returns to the destination preview for another 0.5 seconds.

The last frame remains beneath Seeking/Loading until handoff. The replacement uses an explicit start offset and restores the latest user pause choice. Targets are clamped to the known duration, and seeking becomes available after the first position report. Back cancels playback. Music seeks within its running player instead of replacing the stream.

`PlaybackController` keeps user pause intent separate from decoder feedback. Each `playbackProcess` owns its command queue, so a seek replacement cannot consume commands left for the old decoder. Busy queues retry required pause/resume delivery on the browser tick.

## Video options

SELECT/Tab opens Subtitles, Audio, and Picture. Left/Right changes tabs. Up/Down selects a row. Open applies the choice and closes the picker after success. Back or SELECT closes it without showing playback controls.

A failed change leaves the previous choice active. The active row has an asterisk.

### Picture modes

Original preserves the full encoded frame at its display aspect ratio. Zoom enlarges the center and crops edges. Wider or narrower sources crop to fill 4:3. Near-4:3 sources receive a fixed 4/3 enlargement, allowing removal of bars encoded inside a 4:3 frame. Zoom does not detect black bars and also crops native 4:3 content.

MiSTer and inline Ghostty change picture mode within the running player, including while paused on the same frame. Controls and client-rendered subtitles keep their size. FFplay reloads recorded video at the current position for picture changes. Live TV in FFplay offers only Original.

### Subtitles and audio tracks

Audio lists Server default plus selectable server tracks. Subtitles includes Off. Audio changes and image subtitles such as PGS/VobSub request a new stream at the current position while preserving pause state. Changing or disabling server-burned subtitles also replaces the stream.

MiSTer and inline Ghostty download text subtitles as SubRip and draw them through the shared overlay. Switching downloadable text tracks or Off normally needs no decoder restart. Plex embedded tracks require server burn-in and reload, while Plex sidecar text uses the shared overlay. A failed download keeps the previous text. FFplay requests server burn-in for text too, because shared overlay pixels cannot reach its separate window.

Client text uses the shared [Unicode caption renderer](GO_RENDERING.md#text-coverage), with font fallback, bidirectional layout, shaping, and up to three outlined lines. Complex ASS styling and positioned signs are not reproduced. With a text track selected in the Subtitles tab, LT/RT or J/L adjusts timing in 0.1-second steps within ±10 seconds. Timing changes last for the current playback session. Server-burned subtitles can be cropped by Zoom and have no client timing control.

### Remembered choices

Recorded-video picture, audio, and subtitle choices persist per server, user, and item under `playback` in the state directory. They survive seeks, restart-from-zero, and application restart. New items default to Original, server-default audio, and subtitles Off. Missing tracks fall back safely. A changed media source resets tracks while preserving picture mode. Only started playback and successful changes update saved choices.

Failed writes of picture, audio-track, and subtitle preferences retain the latest choices in memory. A later save retries the write, and shutdown makes a final attempt. [Diagnostics](GO_DIAGNOSTICS.md#events) records `playback.preferences-write` when enabled, without item identifiers or credentials. If the final write fails, those choices cannot survive application exit.

## Loading and buffering

A shared animated overlay covers stream preparation and decoder startup. MPlayer reports its first presented frame immediately. Decoders without that notification use position feedback. Cache-wait feedback shows Buffering over the retained frame. User pause suppresses waiting indicators. FFplay estimates buffering from three seconds without advancing position, and its indicator stays in the companion UI.

Video response headers have a 60-second timeout. Back and replacement seeks cancel pending stream requests. Stop returns to browsing after local decoder cleanup while final server reporting can finish asynchronously.

## Live TV

Selecting a channel tunes it directly. Stop, completion, or failure returns to that channel in the list. Jellyfin negotiates the tuner and transcode through `PlaybackInfo`. Plex discovers enabled DVR channels and tunes a separate consumer through its [Live TV adapter](GO_PLEX.md#live-tv). The client releases the tuner after stop or failure, including cancellation during negotiation. Live channels do not write movie resume or watched state.

MiSTer and inline Ghostty support Original/Zoom and locally decoded captions. Reopening a channel resets picture mode to Original and captions to Off. Live TV has no seeking or timeshift support.

Plex channels with selectable alternate tracks expose Select/Tab → Audio. A selection reloads at the live edge and preserves picture mode and captions. The adapter advertises this capability through `PreparedStream.LiveAudio` and validates `LiveRequest.AudioIndex` against fresh tuner metadata. Jellyfin Live TV audio selection is not implemented. The tested Jellyfin transcode exposed only one audio stream, even where another client exposed alternate broadcast audio.

### Closed captions

When caption data is available, press Select/Tab and select Subtitles → Closed captions. Off hides them immediately. Changes do not reopen the channel. Captions target the primary EIA-608 compatibility text carried in ATSC A53 video data. Full CEA-708 service selection, caption languages, and broadcast styling are not supported. No guide data is required.

MPlayer decodes caption side data from the existing video decoder. The libmpv helper exports decoded subtitle text. Both send complete text updates and clears to the shared overlay. FFplay does not export caption text.

## Transcode configuration

Set conversion limits under `server.transcode` in `settings.json` and restart. The same fields apply to Jellyfin and Plex:

```json
{
  "server": {
    "provider": "jellyfin",
    "url": "http://your-jellyfin-server:8096",
    "transcode": {
      "max_width": 640,
      "max_height": 480,
      "video_bitrate": 8000000
    }
  }
}
```

| Limit | Default | Accepted range |
| --- | --- | --- |
| Maximum width | 720 | 160–1920 pixels |
| Maximum height | 576 | 120–1080 pixels |
| Video bitrate | 12,000,000 | 100,000–50,000,000 bits/sec |

Invalid JSON limits stop startup. Limits apply to recorded video and Live TV for both providers, not original music, photos, or UI dimensions. When `server` is absent, legacy Jellyfin `WIDTHxHEIGHT[@BITRATE]` lines remain supported. Live TV treats bitrate as a streaming budget, so negotiated video bitrate can be lower after audio overhead.

Dimensions preserve source proportions. Lower dimensions can reduce decoding work. Larger accepted values do not guarantee smooth MiSTer playback.

Jellyfin video uses progressive MPEG-2 in MPEG-TS with stereo MP3 at 48 kHz. Recorded video caps at 30 fps for NTSC or 25 fps for PAL. 480i Live TV uses 30000/1001 fps. Target assembly supplies these limits through `playback.Timing`, separately from decoder dimensions. Its zero value uses 30 fps. Current CRT targets select PAL or interlaced NTSC timing explicitly. The player does not force source speed. [Diagnostics](GO_DIAGNOSTICS.md) records requested transcode limits, not measured stream properties.

## Streams and reporting

Go owns authenticated HTTP/TLS. Video reaches decoders through descriptor 3. Controllable music uses a private loopback proxy that forwards byte-range requests for the selected audio stream. Player arguments contain no server URL or credentials. Raw decoder diagnostics are discarded.

Shared playback uses `media.Playback` to prepare and open streams. Each server adapter owns transcode queries, reporting payloads, and tuner release. `media.PreparedStream` binds reporting and cleanup to one attempt. Both adapters pass original audio streams through for music and use shared saved playback choices. See the [media service boundaries](GO_RENDERING.md#media-services).

The Jellyfin implementation groups [preparation and subtitles](../internal/jellyfin/playback.go), [HTTP streaming](../internal/jellyfin/stream.go), [progress reporting](../internal/jellyfin/reporting.go), and [Live TV ownership](../internal/jellyfin/live.go) by responsibility. Both streaming operations validate same-origin URLs. Video allows 60 seconds for response headers and uses a dedicated connection. Audio reuses pooled connections with a 15-second header timeout. Neither imposes a total timeout on the media body.

Session start follows position feedback. Progress and resume updates run every ten seconds and on pause changes. Successful completion near the known end marks recorded video watched. Cancellation does not newly mark it watched, and startup failure preserves its resume position. Final stop/save requests have a five-second deadline. Shutdown waits for bounded outstanding cleanup.

## MiSTer menu music

The optional MiSTer BGM service is separate from music played through Jellyfin or Plex. At startup, the native target contacts `/tmp/bgm.sock` and stops an enabled random/loop playlist. After cleanup, it sends Play only if its earlier Stop was delivered. Restoration does not retain an exact track position. Missing services and command failures leave the client usable.

This behavior has automated socket tests. I do not use the add-on, so restoration still needs testing on hardware that runs it. Ghostty does not contact BGM. [Navigation sounds](GO_CONFIGURATION.md#navigation-sounds) release the audio device before all media playback.

For implementation ownership and native timing constraints, see [architecture](GO_RENDERING.md#external-player-ownership). For remote queues and repeat modes, see [remote control](GO_REMOTE.md).
