# Application settings

Application options belong in one `settings.json` beside `jellyfin.conf`. The standard MiSTer path is `/media/fat/misterfin-crt/settings.json`. The current separate 480i launcher uses `/media/fat/misterfin-crt/interlaced-test/settings.json`. Jellyfin connection details remain in `jellyfin.conf`. Saved sign-in and playback-choice files are application state, not settings to combine.

Copy [settings.example.json](../settings.example.json), or create a file containing only the sections you need:

```json
{
  "ui": {"title": "Trent's CRT", "navigation_sounds": {"enabled": false}},
  "background": {"image": "background.png"},
  "display": {"interlaced": true}
}
```

Sections and their fields can be omitted to keep defaults. An empty object uses all defaults. An explicit `ui.title` of `""` hides the heading. Explicit `false` and zero values retain their documented meaning. Merge changes into the existing file to preserve its other sections. Restart after editing.

## Paths and precedence

The Go executable accepts `-settings /path/to/settings.json`. `MISTERFIN_SETTINGS` supplies the default for that flag. The Ghostty harness accepts `--settings /path/to/settings.json`. A command-line path takes precedence over the environment. An explicitly selected file must exist.

The shared file or legacy files are read once per process. Relative background, music-asset, and diagnostic-log paths resolve beside their source file. A legacy per-section override resolves relative paths beside its own file. The interlaced core remains installed beside `jellyfin.conf`.

Navigation sounds live under `ui.navigation_sounds`. The old top-level `sounds` section remains accepted when the nested setting is omitted. If both exist, the nested object takes precedence as a whole.

The old `music` section remains accepted as an alias for `music_visuals`. If both exist, `music_visuals` takes precedence as a whole.

When `settings.json` exists, its sections are authoritative. Omitted sections use defaults and do not read old JSON files. The legacy `-input-config`, `-sound-config`, `MISTERFIN_INPUT_CONFIG`, `MISTERFIN_SOUND_CONFIG`, and `MISTERFIN_MUSIC_CONFIG` overrides remain supported and take precedence over their corresponding sections. Remove those overrides when migrating to the shared file.

## Migration

If the default `settings.json` is absent, the client reads the old `ui.json`, `background.json`, `display.json`, `sounds.json`, `input.json`, `music.json`, and `diagnostics.json` files beside it. Existing installations continue to work without an immediate configuration change.

To combine those files on the standard MiSTer installation:

```bash
/media/fat/misterfin-crt/misterfin-crt -migrate-settings -config /media/fat/misterfin-crt/jellyfin.conf
```

For the separate 480i installation:

```bash
/media/fat/misterfin-crt/interlaced-test/misterfin-crt -migrate-settings -config /media/fat/misterfin-crt/interlaced-test/jellyfin.conf
```

For local development, run `build/misterfin-crt -migrate-settings -config /path/to/jellyfin.conf`. Migration reads legacy files from the destination settings directory. It moves `sounds` into `ui.navigation_sounds` and renames `music` to `music_visuals`, `default` to `default_background`, and `meters` to `show_audio_meters`. It preserves their values, relative paths, and originals.

Migration refuses to overwrite `settings.json` and rejects malformed legacy JSON or invalid types in legacy music aliases. Migration does not open a display, connect to Jellyfin, or modify sign-in state. Section values are validated during normal startup. After testing the new file, the old settings files can be archived or removed.

## Defaults and recovery

| Setting | Default | Invalid section or unavailable asset |
| --- | --- | --- |
| `ui.title` | `MiSTerFin CRT` when `title` is omitted. An explicit empty string hides the heading. | Restore the default heading with a notice. |
| `ui.navigation_sounds` | Navigation sounds enabled at volume 10. | Disable navigation sounds with a notice. Missing or busy audio devices temporarily suppress feedback. Media volume is unchanged. |
| `background` | Carousel mosaics and item artwork. | Restore normal artwork with a notice. Non-images, unsupported formats, and corrupt images are rejected. |
| `diagnostics` | Off unless `DEBUGLOG` is set. Path: `debug.log`. Limit: 1 MiB per file. | Disable logging with a notice. Write failures stop logging without stopping playback. |
| `music_visuals` | Starfield and stereo meters. Missing optional Toasty sprites are omitted. | Disable backgrounds with a notice. Custom asset failures show a message, and another preset can be selected. Music playback remains available. |
| `input` | Built-in controller mappings. | Stop startup with an error naming the source. Correct the section or omit it to use built-in mappings. |
| `display` | Keep the current Menu core display, normally progressive. | Stop startup instead of silently changing the intended hardware mode. |

A missing default file uses legacy settings or defaults. A malformed settings document, unknown top-level section, unreadable file, or missing explicit settings path stops startup with a file error. The client cannot safely recover display and input intent from a broken document. Within a valid document, section failures follow the table above. Invalid title and navigation-sound values recover independently. If the entire `ui` object is invalid or exceeds its size limit, the heading returns to its default and navigation sounds turn off. Settings files are not rewritten during recovery.

Settings notices appear one at a time for four seconds once browsing is ready. Quick Connect does not consume their display time. Notices do not block navigation or replace an active message. When diagnostics are enabled, each handled fallback records its section name, error category, and selected behavior. Normal defaults do not create failure events. Diagnostic startup failures also report a message on stderr. See [fallback logging](GO_DIAGNOSTICS.md#configuration-fallbacks).

The document must contain one JSON object no larger than 256 KiB. Sections must be objects with known keys. The `input` and `music_visuals` sections are limited to 64 KiB each. Other sections are limited to 4 KiB each. Section limits exclude formatting whitespace in the shared file. Custom artwork has separate image-size limits described in its guide.

Server configuration and sign-in storage errors show retryable setup screens. The default transcode limit remains `720x576@12000000`. Corrupt artwork caches become misses and can be rebuilt. Unwritable caches leave browsing available without disk caching. Missing or damaged playback preferences use defaults. Choices that cannot be saved remain usable during the run, and saving failures are reported at exit.

See the guides for [backgrounds and title](GO_BACKGROUND.md), [sounds](GO_SOUNDS.md), [diagnostics](GO_DIAGNOSTICS.md), [music](GO_MUSIC.md), [input](GO_INPUT.md), and [interlaced display](GO_DISPLAY.md).

## Code ownership

[`internal/settings`](../internal/settings/settings.go) reads startup snapshots and owns the UI schema and legacy names. [Compatibility normalization](../internal/settings/compatibility.go) serves both parsing and [migration](../internal/settings/migration.go). Each component validates its own values and supplies its defaults. [Startup assembly](../cmd/misterfin-crt/paths.go) passes the decoded title and validated music presets to the browser. The browser loads music images on its asset worker and does not parse settings JSON.
