# Configuration

Jellyfin connection details belong in `jellyfin.conf`. Application options belong in one `settings.json` beside it. The standard installation directory is `/media/fat/misterfin-crt`. Restart after changing application settings. Setup Retry reloads only `jellyfin.conf`.

## Server connection

The only required line is the server URL:

```text
http://your-jellyfin-server:8096
```

Approve the displayed Quick Connect code from an already signed-in Jellyfin client. Alternatively, add the API key and username as the next two non-option lines. Blank lines and lines beginning with `#` are ignored. HTTP, HTTPS, and reverse-proxy base paths are supported. URLs must not contain embedded credentials, queries, or fragments.

`PAL`, `NTSC`, `DEBUGLOG`, `INSECURE_TLS`, and a [transcode profile](GO_PLAYBACK.md#transcode-configuration) can appear on separate lines anywhere in the file. PAL is the parser default. Actual output geometry determines the playback frame-rate convention. `PAL`/`NTSC` does not switch the CRT output mode.

TLS certificates are verified by default. `INSECURE_TLS` disables certificate verification for this server, including remote control. Prefer a trusted certificate. Saved sign-in data lives in `session.json` in the application state directory, not beside the server configuration. See [sign-in](GO_BROWSING.md#setup-and-sign-in).

## Application settings

Copy [settings.example.json](../settings.example.json), or include only the sections you need:

```json
{
  "ui": {"title": "MiSTerFin CRT", "navigation_sounds": {"enabled": false}},
  "background": {"image": "background.png"},
  "display": {"interlaced": false}
}
```

Omitted fields use defaults. Preserve other sections when editing. Explicit empty strings, false, and zero values keep their documented meanings.

| Section | Default | Failure behavior |
| --- | --- | --- |
| `ui.title` | `MiSTerFin CRT`. Empty hides the heading. | Restore default title with a notice. |
| `ui.navigation_sounds` | `enabled: true`, `volume: 10`. | Disable sounds with a notice. Media volume is unchanged. |
| `background` | Carousel mosaics and item artwork. | Restore normal artwork with a notice. |
| [`display`](GO_DISPLAY.md) | `interlaced: false`. | Invalid settings stop startup. |
| [`input`](GO_INPUT.md) | Built-in device bindings. | Invalid settings stop startup. |
| [`music_visuals`](GO_MUSIC.md) | Starfield, stereo meters enabled. | Invalid settings disable backgrounds. Missing custom assets leave music playable. |
| [`diagnostics`](GO_DIAGNOSTICS.md) | Off unless `DEBUGLOG` is set. `debug.log`, 1 MiB per file. | Disable logging and report the failure. |

Title and sound failures recover independently. An invalid entire `ui` object restores the title and disables sounds. Notices display for four seconds once browsing is ready. Quick Connect does not consume their display time. Enabled diagnostics records handled failures as `configuration.fallback`. Intentional defaults do not produce failure events. Recovery never rewrites settings.

The file must be one JSON object, at most 256 KiB, with known sections. `input` and `music_visuals` allow 64 KiB each. Other sections allow 4 KiB each, excluding formatting whitespace. A malformed document, unknown top-level section, unreadable file, or missing explicit settings file stops startup because display and input intent cannot be recovered safely.

## Browsing title

`ui.title` changes the carousel and root-list heading. An omitted title uses `MiSTerFin CRT`. `""` or whitespace-only text hides it while keeping the clock. Whitespace is collapsed and control characters are removed. Long titles end in `...` within the heading area, which fits 33 characters at the standard width. Library titles and About keep their own names.

## Browsing background

`background.image` selects one static image for the carousel and browsing lists. An omitted or empty value keeps mosaics and item artwork. Posters, details, About, setup, photos, and playback retain their own presentation.

PNG and JPEG are supported, up to 4 MiB and 2048 pixels per axis. A 4:3 image fits best. The renderer preserves proportions, crops from the center, dims the image, and composites transparency over black. Relative paths resolve beside the settings file. Absolute paths also work.

The client checks image contents, not the extension. Missing files, text, video, unsupported formats, and corrupt images fall back to normal artwork with a notice. The image decodes once at startup. Prepared pixels are cached, and hidden mosaic/backdrop downloads are skipped. Music backgrounds use `music_visuals` instead.

## Navigation sounds

`ui.navigation_sounds.enabled` controls browsing clicks and confirmations. `volume` scales clip amplitude from 0 to 100. Zero silences feedback. The default is 10. These settings do not change music/video volume or the system mixer.

Only visible browsing actions produce cues. Boundaries, redraws, and media controls stay silent. Before playback, the sound worker discards queued cues and releases the audio device. Missing or busy devices suppress feedback without blocking navigation. Invalid sound values disable feedback for that run.

## Paths and precedence

The executable accepts `-settings PATH`. `MISTERFIN_SETTINGS` supplies its default. The harness accepts `--settings PATH`. Flags override the environment. Relative image, music-asset, and log paths resolve beside the file that supplied them. The interlaced core remains beside `jellyfin.conf`.

When `settings.json` exists, omitted sections use defaults rather than legacy files. Legacy `-input-config`, `-sound-config`, `MISTERFIN_INPUT_CONFIG`, `MISTERFIN_SOUND_CONFIG`, and `MISTERFIN_MUSIC_CONFIG` overrides still replace their sections. Remove those overrides when adopting the shared file.

The old top-level `sounds` and `music` sections remain aliases. Explicit `ui.navigation_sounds` and `music_visuals` take precedence as whole sections, even if empty or invalid. In music settings, `default_background` and `show_audio_meters` replace `default` and `meters`. Explicit current fields win, including null values that select their defaults.

## Migration

If the default `settings.json` is absent, the client reads legacy `ui.json`, `background.json`, `display.json`, `sounds.json`, `input.json`, `music.json`, and `diagnostics.json` beside it. To combine them:

```sh
/media/fat/misterfin-crt/misterfin-crt -migrate-settings -config /media/fat/misterfin-crt/jellyfin.conf
```

Use the configuration path of the installation being migrated. Separate 480i test installations can have their configuration under `interlaced-test`. Migration preserves originals and relative paths, writes current names, rejects malformed input, and refuses to overwrite `settings.json`. It does not open a display or connect to Jellyfin. Section values are validated on normal startup. Archive old files after verifying the new settings.

Saved sign-in, playback preferences, and caches are application state and remain separate. [`internal/settings`](../internal/settings/settings.go) owns file loading and compatibility normalization. Each component validates its own values.

## Saved sign-in recovery

If `session.json` in the selected [state directory](GO_BROWSING.md#setup-and-sign-in) is malformed or exceeds 64 KiB, the client preserves the original as `session-damaged-*` in that directory and starts a fresh sign-in. Quick Connect or the connected notice explains the recovery. Storage permission and read errors preserve the original file and show a setup error. Valid credentials survive temporary server failures. Backups request owner-only permissions where supported, contain private sign-in data, and must not be shared.
