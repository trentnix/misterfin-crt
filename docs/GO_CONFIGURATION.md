# Configuration

Connection and application options belong in `settings.json`, normally under `/media/fat/mistervision`. Restart after changing JSON settings. If no `server` section exists, Retry on the setup screen reloads legacy `jellyfin.conf`.

## Server connection

Both providers use the same connection fields:

```json
{
  "server": {
    "provider": "jellyfin",
    "url": "http://your-jellyfin-server:8096",
    "insecure_tls": false,
    "transcode": {
      "max_width": 720,
      "max_height": 576,
      "video_bitrate": 12000000
    }
  }
}
```

For Plex, set `provider` to `plex` and `url` to your Plex Media Server address, normally using port 32400. See [linking and current limits](GO_PLEX.md).

| Field | Default and behavior |
| --- | --- |
| `provider` | `jellyfin`. Accepts `jellyfin` or `plex`. |
| `url` | Required when `server` exists. HTTP, HTTPS, and reverse-proxy base paths are supported. No embedded credentials, query, or fragment. |
| `insecure_tls` | `false`. True disables certificate verification only for the configured media server, including its Jellyfin remote connection. Plex account linking always verifies certificates. |
| `transcode.max_width` | 720. Range: 160–1920 pixels. |
| `transcode.max_height` | 576. Range: 120–1080 pixels. |
| `transcode.video_bitrate` | 12,000,000 bits/sec. Range: 100,000–50,000,000. |
| `jellyfin` | Omitted. Optional `api_key` and `username` must be supplied together. Rejected for Plex. |

Dimensions limit server-side conversion, not UI geometry or picture aspect ratio. Each adapter owns its codecs and frame-rate policy. See [playback](GO_PLAYBACK.md#transcode-configuration).

Without API-key credentials, Jellyfin uses Quick Connect. To use API-key login, add this object inside `server`:

```json
{
  "jellyfin": {
    "api_key": "your-api-key",
    "username": "your-username"
  }
}
```

Keep API-key configuration private. Plex tokens, account identity, and client identifiers remain saved sign-in state rather than configuration. Sign-in files stay in the application state directory.

An explicit `server` section is authoritative. Invalid values stop startup without exposing credentials or falling back to a different server. Only an absent section permits `jellyfin.conf` fallback. If that file is also absent, the client uses a remembered Jellyfin server or offers [local discovery](GO_BROWSING.md#jellyfin-discovery). The legacy file accepts a URL, optional API key and username, `INSECURE_TLS`, `DEBUGLOG`, and `WIDTHxHEIGHT@BITRATE` lines. `PAL` and `NTSC` remain accepted but do not control display or playback timing. The active output geometry determines timing.

## Multiple connections

Keep application settings in one `settings.json`. Add named accounts under `connections.profiles` to use Jellyfin and Plex without separate application configurations:

```json
{
  "connections": {
    "profiles": [
      {
        "id": "home-jellyfin",
        "name": "Home Jellyfin",
        "server": {
          "provider": "jellyfin",
          "url": "http://your-jellyfin-server:8096"
        }
      },
      {
        "id": "home-plex",
        "name": "Home Plex",
        "server": {
          "provider": "plex",
          "url": "http://your-plex-server:32400"
        }
      }
    ]
  }
}
```

Open About with Start/F1, then press Down for Connections. **Use existing connection** lists configured accounts and remembered servers. Jellyfin starts a fresh discovery scan. Plex links your account and lists reachable servers. Both provider choices leave configured profiles available under Use existing connection. Back closes a submenu without changing the active connection. Back from About returns to browsing. If you start another connection and then cancel setup, Back from the connection chooser restores the previous connection and its browsing position. If no connection has succeeded, Back exits setup.

Each profile must have a unique `id`, a display `name`, and validated `server` settings. IDs can contain letters, numbers, underscores, and hyphens, with at most 48 characters. Up to 16 profiles are supported. Invalid profiles stop startup with a configuration error. Changing a name preserves sign-in. Changing the server address or configured account credentials isolates the new sign-in from the old one.

The original top-level `server` and legacy configuration still work and appear as an existing connection. On first launch, they take precedence over named profiles. If neither a configured nor remembered default server exists, the first profile is used. The last successful choice is saved in `connection-choice.json` under the state directory and selected automatically on later launches. Editing connection configuration resets that startup choice. A failed sign-in does not replace it.

Accounts retain independent sign-ins and browsing positions during the application run. Switching cancels the old session's work before activating the next connection. Only the active account receives remote commands or plays media. Display mode, controller mappings, navigation sounds, and backgrounds stay loaded. Switching configured accounts does not restart the application. Restart after editing the configuration file to load new or changed profiles.

Named sign-ins live under `state/connections/<id>-<account digest>/`. A Jellyfin server chosen through About uses `state/discovery/jellyfin/`, separate from the default connection. Plex discovery uses `state/discovery/plex/` for the linked account, remembered server, and separate server credentials. See [Plex discovery](GO_PLEX.md#server-discovery). Navigation positions are held in memory, not saved across application restarts.

## Application settings

Copy [settings.example.json](../settings.example.json), or include only the sections you need:

```json
{
  "ui": {
    "title": "MiSTerVision",
    "navigation_sounds": {
      "enabled": false
    }
  },
  "background": {
    "image": "background.png"
  },
  "display": {
    "interlaced": false
  }
}
```

Omitted fields use defaults. Preserve other sections when editing. Explicit empty strings, false, and zero values keep their documented meanings.

| Section | Default | Failure behavior |
| --- | --- | --- |
| `connections` | Absent: single-server setup. `profiles`: up to 16 named connections. | Invalid profiles stop startup. |
| `server` | Absent: legacy Jellyfin configuration, then a remembered server or discovery if the legacy file is missing. Present: Jellyfin provider, verified TLS, default transcode limits. | Invalid section stops startup. |
| `ui.title` | `MiSTerVision`. Empty hides the heading. | Restore default title with a notice. |
| `ui.show_collections`, `ui.show_playlists` | Both `true`. Empty categories stay hidden. | Restore the invalid option to `true`, with a notice and a diagnostic event. |
| `ui.navigation_sounds` | `enabled: true`, `volume: 10`. | Disable sounds with a notice. Media volume is unchanged. |
| `background` | Carousel mosaics and item artwork. | Restore normal artwork with a notice. |
| [`display`](GO_DISPLAY.md) | `interlaced: false`. | Invalid settings stop startup. |
| [`input`](GO_INPUT.md) | Built-in device bindings. | Invalid settings stop startup. |
| [`music_visuals`](GO_MUSIC.md) | Starfield, stereo meters enabled. | Invalid settings disable backgrounds. Missing custom assets leave music playable. |
| [`diagnostics`](GO_DIAGNOSTICS.md) | Off. Legacy `DEBUGLOG` applies only without `server`. `debug.log`, 1 MiB per file. | Disable logging and report the failure. |

Title, carousel option, and sound failures recover independently. An invalid entire `ui` object restores the title, enables nonempty collections and playlists, and disables sounds.

Notices display for four seconds once browsing is ready. Quick Connect does not consume their display time. Enabled diagnostics records handled failures as `configuration.fallback`. Intentional defaults do not produce failure events. Recovery never rewrites settings.

The file must be one JSON object, at most 256 KiB, with known sections. `input` and `music_visuals` allow 64 KiB each. Other sections allow 4 KiB each, excluding formatting whitespace. A malformed document, unknown top-level section, unreadable file, or missing explicit settings file stops startup because display and input intent cannot be recovered safely.

## Carousel categories

`ui.show_collections` and `ui.show_playlists` default to `true`. A card appears only when the server returns at least one accessible collection or playlist. Empty categories remain hidden even when the server supplies a card. Set either option to `false` to hide its card regardless of contents. Normal libraries and Continue Watching are unaffected. Restart after changing these options.

```json
{
  "ui": {
    "show_collections": true,
    "show_playlists": true
  }
}
```

Values must be booleans. A mistyped value or `null` falls back to `true`, shows a notice, and records a `configuration.fallback` event when diagnostics is enabled. Other valid UI options retain their values.

## Browsing title

`ui.title` changes the carousel and root-list heading. An omitted title uses `MiSTerVision`. `""` or whitespace-only text hides it while keeping the clock. Whitespace is collapsed and control characters are removed. Long titles end in `...` within the heading area, which fits 33 characters at the standard width. Library titles and About keep their own names.

## Browsing background

`background.image` selects one static image for the carousel and browsing lists. An omitted or empty value keeps mosaics and item artwork. Posters, details, About, setup, photos, and playback retain their own presentation.

PNG and JPEG are supported, up to 4 MiB and 2048 pixels per axis. A 4:3 image fits best. The renderer preserves proportions, crops from the center, dims the image, and composites transparency over black. Relative paths resolve beside the settings file. Absolute paths also work.

The client checks image contents, not the extension. Missing files, text, video, unsupported formats, and corrupt images fall back to normal artwork with a notice. The image decodes once at startup. Prepared pixels are cached, and hidden mosaic/backdrop downloads are skipped. Music backgrounds use `music_visuals` instead.

## Navigation sounds

`ui.navigation_sounds.enabled` controls browsing clicks and confirmations. `volume` scales clip amplitude from 0 to 100. Zero silences feedback. The default is 10. These settings do not change music/video volume or the system mixer.

Only visible browsing actions produce cues. Boundaries, redraws, and media controls stay silent. Before playback, the sound worker discards queued cues and releases the audio device. Missing or busy devices suppress feedback without blocking navigation. Invalid sound values disable feedback for that run.

## Paths and precedence

The executable accepts `-settings PATH`. `MISTERVISION_SETTINGS` supplies its default. The harness accepts `--settings PATH`. Flags override the environment. Without either override, settings load beside the `-config` path, which defaults to `jellyfin.conf` in the working directory.

Relative image, music-asset, and log paths resolve beside the file that supplied them. The interlaced core lives beside `settings.json`.

When `settings.json` exists, omitted application sections use defaults rather than legacy JSON files. The absent `server` section is the compatibility exception: it permits `jellyfin.conf`. Legacy `-input-config`, `-sound-config`, `MISTERVISION_INPUT_CONFIG`, `MISTERVISION_SOUND_CONFIG`, and `MISTERVISION_MUSIC_CONFIG` overrides still replace their sections. Remove those overrides when adopting the shared file.

The old top-level `sounds` and `music` sections remain aliases. Explicit `ui.navigation_sounds` and `music_visuals` take precedence as whole sections, even if empty or invalid. In music settings, `default_background` and `show_audio_meters` replace `default` and `meters`. Explicit current fields win, including null values that select their defaults.

## Migration

If the default `settings.json` is absent, the client reads legacy `ui.json`, `background.json`, `display.json`, `sounds.json`, `input.json`, `music.json`, and `diagnostics.json` beside it. The migration command also imports `jellyfin.conf`:

```sh
/media/fat/mistervision/mistervision -migrate-settings -config /media/fat/mistervision/jellyfin.conf
```

Use the configuration path of the installation being migrated. Migration preserves originals and relative asset paths. If `settings.json` already exists without `server`, migration first saves its exact bytes in a private `settings.json.before-server` backup, then adds connection settings atomically.

Explicit `diagnostics.enabled` wins over legacy `DEBUGLOG`. Existing server sections, backups, and files edited after loading cause migration to stop. Invalid connection settings fail before writing. Migration opens no display and contacts no server. Archive old configuration and any credential-bearing backup after verifying the new settings.

The remembered discovery selection (`jellyfin-server.json` in the state directory), saved sign-in, playback preferences, and caches are application state and remain separate. Jellyfin sign-in can also record a stable server ID so [confirmed address recovery](GO_BROWSING.md#jellyfin-discovery) preserves credentials. Explicit server configuration remains authoritative. [`internal/settings`](../internal/settings/settings.go) owns file loading and compatibility normalization. Each component validates its own values.

## Saved sign-in recovery

Jellyfin uses `session.json` in the selected [state directory](GO_BROWSING.md#setup-and-sign-in). Plex uses `plex/session.json`. If either file is malformed or exceeds 64 KiB, the client preserves it as `session-damaged-*` beside the original and starts a fresh sign-in. A notice explains the recovery.

Storage permission and read errors preserve the original file and show a setup error. Valid credentials survive temporary server failures. Backups request owner-only permissions where supported, contain private sign-in data, and must not be shared.
