# Documentation

Start with the [project README](../README.md) for installation and everyday use. These guides describe the current MiSTerFin CRT implementation.

| Guide | Contents |
| --- | --- |
| [Build and install](GO_BUILD.md) | Go and MPlayer builds, release bundles, installation, local tests, and CI. |
| [Configuration](GO_CONFIGURATION.md) | Server connection, settings, title, background, navigation sounds, defaults, and migration. |
| [Display](GO_DISPLAY.md) | Progressive and interlaced output, supported core, recovery, and tested hardware. |
| [Browsing](GO_BROWSING.md) | Quick Connect, Continue Watching, lists, photos, About, and artwork caches. |
| [Playback](GO_PLAYBACK.md) | Players, controls, seeking, subtitles, audio tracks, picture modes, and Live TV. |
| [Input](GO_INPUT.md) | Controller profiles, axes, labels, and held-button behavior. |
| [Music](GO_MUSIC.md) | Queues, shuffle, meters, and custom visual backgrounds. |
| [Remote control](GO_REMOTE.md) | Jellyfin commands, queue behavior, and the control interface. |
| [Diagnostics](GO_DIAGNOSTICS.md) | Logging, recovery events, privacy, and troubleshooting. |
| [Architecture](GO_RENDERING.md) | Application ownership, shared rendering, output backends, and decoder interfaces. |
| [Third-party notices](THIRD_PARTY.md) | Attribution, licenses, and external components. |

## Current limits

Update availability is displayed, but installation is a placeholder. Search, automatic photo slideshows, photo zoom, and Live TV audio-track selection are not implemented. Live TV has no seeking or timeshift support. The separate-window FFplay fallback has fewer controls than MiSTer and inline Ghostty. See the [playback guide](GO_PLAYBACK.md).

PAL/576i, direct MiSTer YPbPr output, Zaparoo DDR integration, and hardware validation of optional MiSTer background-music restoration are deferred. See [tested display scope](GO_DISPLAY.md#tested-scope) and [menu music](GO_PLAYBACK.md#mister-menu-music). Screenshot capture and exact reproduction of C music visualizers are outside the current scope.

The original port plan and C hardware notes are preserved in Git history. They are not installation instructions for this client. The C reference is the [MiSTerFin integration repository](https://github.com/trentnix/MiSTerFin), branch `local-all-features`, starting at `19d99fa5f479692e45ea7b5dddc42e42fb1782a9`.
