# Diagnostics

Diagnostics records startup, requests, and playback milestones. It is off by default and uses the same implementation on MiSTer and desktop.

## Enable and collect logs

Add this section to `settings.json`, restart, reproduce the issue, then exit normally:

```json
{"diagnostics": {"enabled": true, "path": "debug.log", "max_bytes": 1048576}}
```

Collect `debug.log` and `debug.log.1` before launching again. Each process clears its log pair at startup. Relative paths resolve beside the settings file. The standard MiSTer log is `/media/fat/misterfin-crt/debug.log`. An absolute path can select other storage, including `/tmp`.

Without a JSON `server` section, `DEBUGLOG` on a line in legacy `jellyfin.conf` enables the same defaults. With `server` configured, only the diagnostics section controls logging. Explicit `diagnostics.enabled` overrides it. To disable logging, set false, or omit the section and remove `DEBUGLOG`.

`max_bytes` applies to each file and accepts 4,096–67,108,864 bytes. Use a dedicated path because startup truncates it and owns its `.1` companion. Only one application instance can use a log path.

The interlaced supervisor has an additional pair, `debug.log.supervisor` and `debug.log.supervisor.1`. Collect both pairs for a 480i startup or exit problem. Each file uses the same limit, so defaults retain up to 4 MiB in interlaced mode. If the child never starts, its existing log can be from a previous run. Check timestamps.

## Events

Each line is JSON with a timestamp and event name in `msg`.

| Event family | Information |
| --- | --- |
| `application.start`, `.exit`, `.phase`, `.failure` | Build/platform, application or supervisor role, startup stage, elapsed time, and failure category. |
| `application.display` | Logical and physical dimensions. |
| `input.backend`, `.device`, `.unavailable` | Backend, initial devices and bindings, and identification/open failures. No button presses. |
| `mister.display`, `.framebuffer`, `.setting`, `.settings` | Interlaced state, kernel framebuffer geometry, and allowlisted numeric INI settings. |
| `update.start`, `.end`, `.recovered`, `.restart` | Installation start, completion flags for failure/cancellation/recovery, startup rollback, and a restart request after successful cleanup. No download URLs or raw errors. |
| `configuration.fallback` | Logical setting, safe error category, and selected recovery behavior. |
| `authentication.session-recovered` | Damaged saved sign-in was backed up and replaced. No file contents or paths. |
| `http.request`, `remote.socket` | Endpoint/status/timing or WebSocket connection result. No query strings or credentials. |
| `browser.page`, `.home` | Accepted page/feed results, bounded identifiers, counts, and failures. |
| `playback.start`, `.phase`, `.prepared` | Decoder, preparation stages, resume offset, and requested transcode limits. |
| `playback.first-position`, `.first-frame` | Separate milestones for position feedback and first presented frame feedback. |
| `playback.pause`, `.buffering`, `.progress` | State transitions and ten-second progress summaries. |
| `playback.decoder-exit`, `.end` | Exit/signal, cancellation, elapsed time, and final stage. |
| `playback.preferences-write` | A failed preferences write, retained for retry. No item identifiers or paths. |
| `diagnostics.dropped` | Events discarded when the writer queue filled. |

For slow startup, compare request timing, `stream-open`, `decoder-start`, and first-frame feedback. For seeks, follow the new process-local `playback` counter. Cancellation can mean Stop, a superseded seek, or exit, rather than failure. First-frame feedback is not a measurement of light from the CRT.

If picture or track choices disappear after restart, look for `playback.preferences-write` and check that the state directory is writable. A later save retries failed writes, and shutdown makes a final attempt. An unexpected sign-in prompt with `authentication.session-recovered` indicates [damaged saved sign-in](GO_CONFIGURATION.md#saved-sign-in-recovery). Do not share the backup file.

Metadata/artwork timings include buffered body reads. `/media-stream` measures opening through response headers, not the whole stream. `/audio-stream` includes one proxy request through completion. GitHub release checks are outside the Jellyfin request log.

The MiSTer inventory reads numeric display settings from `/media/fat/MiSTer.ini`, preserving section identity. It does not resolve alternate INI files or prove active signal timing. Reads are bounded to 128 KiB and 64 setting events.

## Configuration fallbacks

Handled invalid title, background, sound, and music settings record one event when recovery occurs. Missing custom assets also record their fallback. Intentional omissions, empty values, and disabled settings are not failures.

```json
{"msg":"configuration.fallback","configuration":"ui","error_kind":"invalid","fallback":"default-title"}
```

Invalid diagnostics settings or file-open failures disable logging, write a short stderr message, and queue a settings notice. No alternate log file is opened. Malformed `settings.json` can stop startup before its diagnostics section is usable. Invalid command-line arguments use stderr. Preview commands do not enable logging.

## Limits and privacy

A 128-entry queue and one writer keep disk I/O off playback and UI loops. A full queue drops events. Normal exit drains accepted events. Abrupt termination can lose them. Write failures disable logging without stopping playback.

Logs exclude server origins, queries, authorization, response bodies, raw network/player errors, media titles, captions, and Quick Connect secrets. They can contain item/user identifiers in endpoint paths, device names, build details, numeric display settings, and playback timing. Review logs before sharing. Files request owner-only permissions where supported.

Diagnostics does not measure dropped frames, rendered FPS, decoder load, or A/V drift. Those require player instrumentation and hardware checks. Position and buffering events alone cannot establish smooth playback.
