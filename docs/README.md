# Documentation

Start with the [project README](../README.md) for installation and everyday use. These guides describe the current MiSTerVision implementation.

| Guide | Contents |
| --- | --- |
| [Build and install](GO_BUILD.md) | Go and MPlayer builds, release bundles, installation, local tests, and CI. |
| [Configuration](GO_CONFIGURATION.md) | Server connection, settings, title, background, navigation sounds, defaults, and migration. |
| [Display](GO_DISPLAY.md) | Progressive and interlaced output, supported core, recovery, and tested hardware. |
| [Browsing](GO_BROWSING.md) | Sign-in, Continue Watching, lists, photos, About, and artwork caches. |
| [Plex](GO_PLEX.md) | Account linking, supported libraries, streaming, Live TV, and provider-specific limits. |
| [Playback](GO_PLAYBACK.md) | Players, controls, seeking, subtitles, audio tracks, picture modes, and Live TV. |
| [Input](GO_INPUT.md) | Controller profiles, axes, labels, and held-button behavior. |
| [Music](GO_MUSIC.md) | Queues, shuffle, meters, and custom visual backgrounds. |
| [Remote control](GO_REMOTE.md) | Jellyfin commands, queue behavior, and the control interface. |
| [Diagnostics](GO_DIAGNOSTICS.md) | Logging, recovery events, privacy, and troubleshooting. |
| [Architecture](GO_RENDERING.md) | Application ownership, shared rendering, output backends, and decoder interfaces. |
| [Third-party notices](THIRD_PARTY.md) | Attribution, licenses, and external components. |

## Current limits

Search, server discovery, automatic photo slideshows, and photo zoom are not implemented. Plex Home profile switching and Plex remote control are not implemented. Jellyfin Live TV audio-track selection is unavailable. Plex exposes alternate live audio when the stream provides it. Live TV has no seeking or timeshift support.

The separate-window FFplay fallback has fewer controls than MiSTer and inline Ghostty. See [Plex limits](GO_PLEX.md#playback-and-limits) and the [playback guide](GO_PLAYBACK.md).

PAL 288p/576i, direct MiSTer YPbPr output, and optional MiSTer background-music restoration need testing by someone with suitable hardware. Zaparoo DDR integration is not implemented. See [tested display scope](GO_DISPLAY.md#tested-scope) and [menu music](GO_PLAYBACK.md#mister-menu-music). Screenshot capture and exact reproduction of C music visualizers are outside the current scope.

The original port plan and C hardware notes are preserved in Git history. They are not installation instructions for this client. The C reference is the [MiSTerFin integration repository](https://github.com/trentnix/MiSTerFin), branch `local-all-features`, starting at `19d99fa5f479692e45ea7b5dddc42e42fb1782a9`.
