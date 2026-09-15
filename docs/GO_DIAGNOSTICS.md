# Diagnostics

Diagnostics records request results, startup details, and playback milestones for troubleshooting. It is disabled by default and uses the same implementation on MiSTer and the desktop harness.

## Enable logging

Copy [diagnostics.example.json](../diagnostics.example.json) beside the `jellyfin.conf` used by the launcher, naming the copy `diagnostics.json`:

```json
{
  "enabled": true,
  "path": "debug.log",
  "max_bytes": 1048576
}
```

Restart the application, reproduce the issue, then exit normally to flush accepted events. Collect `debug.log` and `debug.log.1` if the latter exists. A fresh application launch clears its two logs, so copy them before launching again.

On the ordinary MiSTer installation, the default log is `/media/fat/misterfin-crt/debug.log`. The 480i installation currently uses `/media/fat/misterfin-crt/interlaced-test/debug.log` because its `jellyfin.conf` is in that directory. The Ghostty harness writes beside the configuration supplied to the client. Relative log paths resolve beside `diagnostics.json`. An absolute path can place the log elsewhere, including `/tmp` to avoid SD-card writes.

Adding `DEBUGLOG` on its own line in `jellyfin.conf` also enables logging with these defaults. An explicit `enabled` value in `diagnostics.json` overrides that switch. To disable logging, set `enabled` to `false`, or remove both the JSON file and the `DEBUGLOG` line.

`max_bytes` limits each file and must be between 4,096 and 67,108,864 bytes. The default retains at most two 1 MiB files. Choose a dedicated log path because the logger truncates that file on launch and owns its `.1` companion. Only one application instance can use a given log path at a time.

The 480i display supervisor uses a separate log at the configured path plus `.supervisor`, with `.supervisor.1` for rotation. Each supervisor file uses the same `max_bytes` limit. This keeps core-switch and restoration failures separate from the child application, which opens its own log. When diagnosing a 480i launch or exit failure, collect both pairs. Each process clears its own pair on launch. If the supervisor fails before starting the application, an existing application log can be from an earlier run. Check timestamps. An interlaced run can retain up to four files, totaling 4 MiB with the defaults.

## What the log contains

Each line is a JSON object with a timestamp and an event name in `msg`.

| Events | Recorded information |
| --- | --- |
| `application.start`, `application.exit` | Build revision, OS/architecture, headless/native selection, application or display-supervisor role, elapsed runtime, and failure/cancellation flags. |
| `application.phase`, `application.failure` | Startup stage and elapsed time, or the last stage and a safe error category on failure. Includes display configuration, framebuffer opening, browser/input/sound configuration, input opening, and browser execution. The supervisor reports failures from its core-switch, child-process, and restoration lifecycle under `interlaced-supervisor`. |
| `application.display` | Logical UI and physical framebuffer dimensions after opening the display. |
| `input.backend`, `input.device`, `input.unavailable` | Terminal/native selection, configured profile count, initial evdev node names, device names, virtual-input status, binding replacement, mapped-button/axis and trigger counts, or open/identification failures. No button presses or hotplug polls are logged. |
| `mister.display`, `mister.framebuffer` | Whether the interlaced child is active and the kernel framebuffer's numeric format, swap, width, height, and stride before the application opens it. Missing or unreadable mode data is reported without raw error text. |
| `mister.setting`, `mister.settings` | Allowlisted numeric display settings with their INI section, entry/rejection counts, and read/limit status. |
| `http.request` | Method, endpoint path, HTTP status, elapsed milliseconds, received bytes, and failure status. Status zero means no HTTP response was received. Query strings and origins are excluded. |
| `playback.start`, `playback.phase`, `playback.prepared` | Decoder protocol, metadata/stream/gate/decoder milestones, resume offset, Live TV status, and numeric transcode limits when present. |
| `playback.first-position`, `playback.first-frame` | Elapsed time until position feedback and the decoder's first-frame notification. They are different milestones. |
| `playback.pause`, `playback.buffering` | Pause changes and distinct buffering-state notifications. |
| `playback.progress` | Position and pause state every ten seconds while the decoder is monitored. |
| `playback.decoder-exit`, `playback.end` | Numeric exit code and signal, failure/cancellation flags, elapsed time, and the last preparation/playback stage. |
| `diagnostics.dropped` | Number of discarded entries when the writer fell behind. |

MiSTer settings are read once per process from `/media/fat/MiSTer.ini`. The inventory includes `ypbpr`, `composite_sync`, `forced_scandoubler`, `vga_scaler`, `direct_video`, `vsync_adjust`, `video_mode`, `video_mode_ntsc`, and `video_mode_pal` in the top-level, `[MiSTer]`, `[Menu]`, and `[MiSTerFinInterlaced]` sections. It preserves section identity and does not resolve precedence or alternate MiSTer INI files. These are configured values, not proof of the active signal timing. Other sections, unrelated keys, comments, and nonnumeric values are excluded. Reads are limited to 128 KiB and 64 setting events. Overlong lines stop the inventory and report a read failure.

Logging begins after command-line parsing and before framebuffer or browser setup. Invalid `display.json` is reported once the logger opens. The `DEBUGLOG` switch is discovered independently of Jellyfin configuration validity, within the first 1 MiB and subject to a 64 KiB line limit. Invalid command-line arguments, invalid diagnostics settings, and failures to open the log still use stderr. Preview commands do not enable diagnostics. After the browser starts, a failure tagged `browser` can include normal resource cleanup. It does not identify a specific cleanup operation.

The `playback` field is a process-local counter that distinguishes overlapping seek replacements. It is not a Jellyfin session identifier. Playback elapsed times start when that decoder request begins. The first-frame event reflects player feedback, not a measurement of light emitted by the CRT. Not every decoder supplies first-frame or buffering notifications.

Metadata and artwork requests include buffered-body read time. `/media-stream` represents opening video or native audio through response headers, with zero body bytes because the body continues streaming afterward. `/audio-stream` represents one desktop audio-proxy request through completion, including bytes copied. Those durations have different meanings. Non-success response bodies are not read just to count bytes. The GitHub release check is outside this Jellyfin request log.

For a slow launch, compare metadata requests, `stream-open`, `decoder-start`, and `playback.first-frame`. For a failed seek, follow the new `playback` counter and its final stage. Cancellation can mean a user stop, a superseded seek, or application exit. It does not necessarily indicate an error. Process termination can produce a nonzero decoder exit even when cancellation was expected.

## Performance and privacy

The logger uses a 128-entry queue and one worker for JSON encoding and disk writes. Producers never wait for disk. A full queue drops diagnostic entries instead of delaying media or the UI. The application drains accepted entries on normal exit. Abrupt termination can lose queued entries. A file-write failure disables logging. Failure to open or write the log does not stop playback, and a short message reports the problem on stderr. Invalid JSON settings remain configuration errors.

Request logging excludes origins, query strings, authorization headers, bodies, and raw network errors. Stream URLs, media titles, captions, Quick Connect codes, and raw player output are never logged. Endpoint paths can still contain item or user identifiers, and logs expose playback timing, local build details, hardware device names, and numeric display settings. Files request owner-only permissions where the filesystem supports them.

The diagnostic log does not yet measure dropped frames, decoder load, rendered FPS, or audio/video drift. Position summaries and buffering events help locate a problem but cannot prove smooth frame presentation. Those measurements require separate player instrumentation.
