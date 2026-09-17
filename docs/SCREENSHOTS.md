# Screenshots

These images show the interface included in v1.2.0. Browsing images were captured from the desktop headless client using Jellyfin. Setup images use the production renderer with fictional names, addresses, and approval codes. Alex’s sample avatar is the project logo, not a real account image.

The shared UI uses a 640×240 logical frame for NTSC. These PNGs double its rows to show the intended 4:3 proportions at 640×480. They do not show CRT scanout, interlace, animation, or video smoothness. Browsing captures show keyboard hints. Setup previews show the default MiSTer controller hints.

## Browsing

| Home carousel | Continue Watching |
| --- | --- |
| ![Library carousel with artwork mosaics](images/screenshots/home-carousel.png) | ![Resumable movies and TV episodes](images/screenshots/continue-watching.png) |

| Movie list | Movie details |
| --- | --- |
| ![Movie titles beside the selected poster](images/screenshots/movies-list.png) | ![Movie artwork, description, and Play action](images/screenshots/movie-info.png) |

## Connections and sign-in

Open About with Start on a controller or F1 on a keyboard, then press Down for Connections. **Use existing connection** appears only when a configured, remembered, or current connection is available.

| Connection choices | Jellyfin discovery |
| --- | --- |
| ![Connect to your media with saved connections and provider choices](images/screenshots/connections.png) | ![A single discovered Jellyfin server with no unnecessary navigation hint](images/screenshots/jellyfin-discovery.png) |

| Jellyfin Quick Connect | Plex account linking |
| --- | --- |
| ![Quick Connect with an example approval code](images/screenshots/quick-connect.png) | ![Plex linking with an example code](images/screenshots/plex-link.png) |

| Plex server selection | Setup help |
| --- | --- |
| ![Plex server address and Sign in with another account](images/screenshots/plex-servers.png) | ![Discovery failure with retry and configuration guidance](images/screenshots/setup-help.png) |

See [Jellyfin setup](GO_BROWSING.md#setup-and-sign-in), [Plex discovery](GO_PLEX.md#server-discovery), and [saved connections](GO_CONFIGURATION.md#multiple-connections).

## Plex Home

The profile picker shows three cards at a time. Left/Right scrolls through additional viewers, and a counter shows the selection’s position. Protected viewers use the controller or keyboard keypad. The fourth digit submits the PIN.

| Viewing profiles | PIN entry |
| --- | --- |
| ![Three visible profile cards with a position counter for four viewers](images/screenshots/plex-profiles.png) | ![Avatar and name above the PIN prompt and numeric keypad](images/screenshots/plex-pin.png) |

![About with the current viewer, Switch profile, Connections, and update controls](images/screenshots/about.png)

See [Plex Home behavior](GO_PLEX.md#plex-home-profiles) for remembered viewers, retry messages, cancellation, and permissions.

## Refreshing previews

From the repository root, regenerate the nine setup and About images with:

```sh
DOCS_PREVIEW_DIR="$PWD/docs/images/screenshots" \
  go test ./internal/rendering -run '^TestDocumentationPreviews$' -count=1
```

The [preview fixtures](../internal/rendering/docs_preview_test.go) use the current `RasterRenderer`. The command needs no server, account, credentials, or MiSTer. Ordinary tests skip the exporter. Review each image after changing layouts, and keep fixture text aligned with the connection flow.

The four browsing captures require the [desktop harness](../tools/ghostty/README.md) and a media library. Use isolated development state when capturing them. Export complete frames, preserve the physical 4:3 proportions, and check for private account details before adding images to the repository. Do not include real sign-in codes or credential files.
