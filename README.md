# MiSTerVision

<p align="center">
  <img src="docs/images/mistervision-logo.png" alt="MiSTerVision logo" width="256" height="256">
</p>

MiSTerVision is a Jellyfin and Plex client for CRT televisions on MiSTer FPGA. It supports movies, TV shows, live TV, music, photos, collections, and playlists through one interface.

Both providers share browsing and playback controls, server discovery, and saved connection switching. Jellyfin can switch saved Quick Connect users. Plex supports Home profiles with avatars and PIN entry. About also lets you forget a Jellyfin user or sign out of Plex on this device.

My goal is a great media experience on CRTs. I test and use MiSTerVision on a MiSTer connected to a consumer 4:3 CRT television, not a PVM or an HD set. I have tested both server providers, including Jellyfin 12.

![MiSTerVision library carousel](docs/images/screenshots/home-carousel.png)

## Run on MiSTer

These instructions cover v1.3.0. Existing MiSTerVision installations can update through About.

For a new installation, download `mistervision-vX.Y.Z-mister.zip` from the [latest release](https://github.com/trentnix/mistervision/releases/latest). Extract the ZIP and copy these files to the SD card. Keep the launcher filename free of spaces. Make the launcher and both binaries executable if your filesystem requires it. If upgrading an existing installation manually, exit the application first and keep your configuration and state files.

| File | Destination |
| --- | --- |
| `mistervision/mistervision` | `/media/fat/mistervision/mistervision` |
| `mistervision/mplayer-arm` | `/media/fat/mistervision/mplayer-arm` |
| `Scripts/MiSTerVision.sh` | `/media/fat/Scripts/MiSTerVision.sh` |

Copy the remaining files from the ZIP’s `mistervision` directory into `/media/fat/mistervision/`. The archive includes examples, notices, and version information but no active configuration or saved state. Its `INSTALL.txt` has detailed instructions. To build from source, follow the [build guide](docs/GO_BUILD.md).

If moving from MiSTerFin CRT, follow the [rename instructions](docs/GO_BUILD.md#moving-from-misterfin-crt) before installing. This rename requires a manual installation.

## Connect to a server

Launch **MiSTerVision** from the Scripts menu. With no server configured, MiSTerVision starts Jellyfin discovery. An explicit `server` section or legacy `jellyfin.conf` takes precedence.

- **Jellyfin:** Select a discovered server, then approve the displayed Quick Connect code in a signed-in Jellyfin client. See [discovery troubleshooting](docs/GO_BROWSING.md#jellyfin-discovery) if no server appears.
- **Plex:** Open About with Start, press Down for **Connections**, and choose **Plex**. Approve the code at [plex.tv/link](https://plex.tv/link). If you use Plex Home, choose a viewer and enter their PIN when requested. Choose a server to connect. Plex prefers a reachable local address. Linking and Home profile checks require internet access.

The last successful connection opens automatically on later launches. If a discovered server changes address, the client can find the same server and ask before reconnecting. Explicitly configured addresses stay fixed.

### Specify an address

For a remote Jellyfin server or an explicit Jellyfin or Plex address, copy [settings.example.json](settings.example.json) to `/media/fat/mistervision/settings.json` and set your server address. A minimal Jellyfin configuration is:

```json
{
  "server": {
    "provider": "jellyfin",
    "url": "http://your-jellyfin-server:8096"
  }
}
```

For Plex, use:

```json
{
  "server": {
    "provider": "plex",
    "url": "http://your-plex-server:32400"
  }
}
```

Sign-ins, playback choices, and artwork caches stay separate for each provider, server, and viewer. See [configuration](docs/GO_CONFIGURATION.md) and [Plex limits](docs/GO_PLEX.md).

### Switch connections or users

Open **About → Connections → Use existing connection** to return to a configured or remembered server. That option appears only when a connection is available. You can keep Jellyfin and Plex signed in, but only the active connection plays media or accepts remote commands. Back cancels a new connection attempt and lets you return to the previous browser.

For Jellyfin, press Up in About for **Switch profile** when multiple users are saved. With only one user, About offers **Add user** directly. **Add user** opens Quick Connect. Approve the code while signed in as the user you want to add. Later launches reopen the last selected user. API-key connections keep their configured user. See [Jellyfin user switching](docs/GO_BROWSING.md#jellyfin-users).

For Plex Home, press Up in About for **Switch profile** when more than one profile is available. The viewer’s avatar and name appear on the carousel and About. Protected viewers must enter their PIN again after an application restart. To change the linked Plex account, choose **Sign in with another account** on **Choose a Plex server**.

Back from a profile picker opened through About returns to About without changing the active user.

To remove a saved Jellyfin user, select **Forget user** in About or the user picker. To log out of Plex, select **Sign out** in About. Both actions require confirmation and remove the saved sign-in only from this connection on this device. They do not delete the server account or media. Connecting to Plex again requires account linking.

Jellyfin users and Plex Home viewers are different from named server connections in `connections.profiles`. No JSON is needed for Home viewers. See [Plex Home](docs/GO_PLEX.md#plex-home-profiles) and [multiple server connections](docs/GO_CONFIGURATION.md#multiple-connections).

## Updates

The client checks for the latest public release at startup. An available update appears beneath the carousel title. Use **Check updates** in About to check again.

1. While browsing, press Start on a controller or F1 on a keyboard to open **About**.
2. Select **View release** and review the changes.
3. Select **Install** and wait for completion. The app restarts automatically after a successful update.

Updates replace the application, matching MPlayer, and standard launcher together. Your settings, sign-in, playback preferences, cached artwork, and optional interlaced core are preserved. Back cancels during download or validation. During installation, wait for completion. Failed replacements restore the previous files. Interrupted replacements recover at the next startup.

Automatic updates require the standard installation paths above. Desktop and custom installations use manual installation. See [manual installation and recovery](docs/GO_BUILD.md#application-updates) for details.

## Progressive and interlaced output

The default uses MiSTer’s current display mode, normally 240p for NTSC or 288p for PAL. Interlaced output is optional: 480i for NTSC or 576i for PAL. I have tested 240p and 480i. Someone with PAL hardware will need to validate 288p and 576i output.

To enable interlaced output:

1. Exit MiSTerVision. Install the matching client and MPlayer builds described above.
2. Download the supported **InterlacedMenu.rbf v0.0.1** from the [display guide](docs/GO_DISPLAY.md#enable-or-disable-interlaced-output). Place it at `/media/fat/mistervision/InterlacedMenu.rbf`.
3. Set the `display` section in `/media/fat/mistervision/settings.json`:

```json
{
  "display": {
    "interlaced": true
  }
}
```

Launch **MiSTerVision** from the normal Scripts menu. The application switches to the interlaced core and restores the normal menu when you exit. Synchronization is automatic. The same launcher works for both modes.

To return to the progressive default, exit the application and set `display.interlaced` to `false`:

```json
{
  "display": {
    "interlaced": false
  }
}
```

Omitting the `display` section also restores the default on the next launch. Preserve other sections when changing this setting. The [display guide](docs/GO_DISPLAY.md) explains core verification, the scoped `MiSTer.ini` changes and backup, and hardware requirements.

## Controls

I test with an Xbox controller. The default layout follows MiSTer: B selects, plays, or pauses, and A goes back, cancels, or stops. Use the D-pad or left analog stick to navigate. During video or music playback, any direction shows or hides controls. Triggers seek, and shoulder buttons change music tracks.

Controller mappings are configurable in the `input` section of `settings.json`. For example, if you prefer A to select and B to go back, add this section while preserving your other settings:

```json
{
  "input": {
    "profiles": [
      {
        "match": "*Xbox*",
        "buttons": {
          "304": "open",
          "305": "back"
        }
      }
    ]
  }
}
```

The numbers are Linux input event codes. For a standard Xbox mapping, `304` is A (`BTN_SOUTH`) and `305` is B (`BTN_EAST`). Other controllers or drivers can report different codes. Use an input inspector such as `evtest` to read the code when you press a button. See [finding device names and button codes](docs/GO_INPUT.md#finding-device-names-and-button-codes).

The case-sensitive `match` pattern must match the controller’s Linux device name. Restart after editing. Unspecified bindings keep their defaults, and on-screen hints follow the mappings. The [input guide](docs/GO_INPUT.md) covers axes and custom labels. These profiles configure MiSTer hardware input. Ghostty uses terminal keyboard controls.

The [playback guide](docs/GO_PLAYBACK.md#playback-controls) lists controller and keyboard controls. [About](docs/GO_BROWSING.md#about-and-updates) shows the installed version and provides [updates](#updates).

To control playback from another Jellyfin client, select **MiSTerVision** as the playback device. Remote play, queues, pause/resume, seeking, shuffle, and repeat are supported. See [remote control](docs/GO_REMOTE.md).

## Screenshots

These images use the current shared renderer. Browsing captures come from the desktop harness. Setup previews use example names, addresses, approval codes, and a sample avatar. Controller and keyboard hints follow the active input device.

| Continue Watching | Movie details |
| --- | --- |
| ![Continue Watching with saved playback positions](docs/images/screenshots/continue-watching.png) | ![Movie artwork, summary, runtime, and playback controls](docs/images/screenshots/movie-info.png) |

| Choose a connection | Discover Jellyfin |
| --- | --- |
| ![Saved connections and Jellyfin or Plex setup](docs/images/screenshots/connections.png) | ![A discovered Jellyfin server with its name and address](docs/images/screenshots/jellyfin-discovery.png) |

| Plex Home viewers | Protected profile |
| --- | --- |
| ![Three visible Plex Home cards and a counter for four viewers](docs/images/screenshots/plex-profiles.png) | ![Viewer avatar and name above the centered PIN keypad](docs/images/screenshots/plex-pin.png) |

| Saved Jellyfin users | About and Plex sign-out |
| --- | --- |
| ![Saved Jellyfin users with Add user and Forget user controls](docs/images/screenshots/jellyfin-users.png) | ![About with Switch profile, Sign out, Connections, and update controls](docs/images/screenshots/about.png) |

The [full gallery](docs/SCREENSHOTS.md) also shows account linking, server selection, the movie list, setup help, and About.

## Configuration

Connection and application settings live in **`settings.json`**, normally `/media/fat/mistervision/settings.json` on MiSTer. Both providers use `server.provider`, `server.url`, `server.insecure_tls`, and `server.transcode`. For a new installation, copy [settings.example.json](settings.example.json). Existing installations can use the migration command below. Omitted optional fields use defaults. Restart after changing settings.

```json
{
  "server": {
    "provider": "jellyfin",
    "url": "http://your-jellyfin-server:8096"
  },
  "ui": {
    "title": "MiSTerVision",
    "show_collections": true,
    "show_playlists": true,
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

| Setting | Defaults and options | Guide |
| --- | --- | --- |
| `connections.profiles` | No configured entries. Discovered connections are remembered automatically. Add up to 16 named server connections. | [Connections](docs/GO_CONFIGURATION.md#multiple-connections) |
| `server` | Provider: `jellyfin`. URL required when the section exists. TLS verified. Transcode limits: 720×576 at 12 Mbps. | [Connection](docs/GO_CONFIGURATION.md#server-connection) |
| `ui.title` | Heading: `MiSTerVision`. An explicit empty `title` hides it. Long titles are truncated. | [Title](docs/GO_CONFIGURATION.md#browsing-title) |
| `ui.show_collections`, `ui.show_playlists` | Both `true`. Show nonempty categories. Set either to `false` to hide its card. | [Carousel](docs/GO_CONFIGURATION.md#carousel-categories) |
| `ui.navigation_sounds` | `enabled: true`, `volume: 10` out of 100. False or volume zero silences navigation sounds. | [Sounds](docs/GO_CONFIGURATION.md#navigation-sounds) |
| `background` | Generated carousel mosaics and item artwork on lists. `image` selects one custom background. | [Background](docs/GO_CONFIGURATION.md#browsing-background) |
| `display` | `interlaced: false`. Keep the current display, normally progressive. | [Display](docs/GO_DISPLAY.md) |
| `input` | Built-in controller mappings and button labels. Profiles override matching devices. | [Input](docs/GO_INPUT.md) |
| `music_visuals` | Music playback appearance only. `default_background: "Starfield"`, `show_audio_meters: true`. Missing optional Toasty sprites are omitted. | [Music visuals](docs/GO_MUSIC.md) |
| `diagnostics` | Off. Legacy `DEBUGLOG` applies only without a `server` section. Path: `debug.log`. Limit: 1 MiB per file. | [Diagnostics](docs/GO_DIAGNOSTICS.md) |

Existing installations can retain legacy settings or [migrate them into one file](docs/GO_CONFIGURATION.md#migration). Migration preserves the original files. Invalid connection settings stop startup, while recoverable UI settings use the defaults documented in the [configuration guide](docs/GO_CONFIGURATION.md#application-settings).

### Browsing background

To use one custom image on the carousel and browsing lists, set `background.image` in `settings.json`:

```json
{
  "background": {
    "image": "background.png"
  }
}
```

Place the image in the same directory, or use an absolute path. PNG and JPEG are supported, up to 4 MiB and 2048 pixels in either dimension. A 4:3 image fits best. The client crops and dims it to keep the interface readable. Restart to apply changes. An omitted or empty `image` keeps the normal artwork.

The client checks the file contents, not its extension. A video, text file, unsupported image format, or corrupt image is rejected. Missing or invalid image files fall back to the normal artwork with a brief on-screen notice. With diagnostics enabled, the fallback also records a `configuration.fallback` event. Startup continues.

### Sounds and caches

To turn off navigation sounds, set `ui.navigation_sounds`:

```json
{
  "ui": {
    "navigation_sounds": {
      "enabled": false
    }
  }
}
```

Sound settings affect browsing feedback only. They do not change music or video volume.

`MISTERVISION_CACHE_ROOT` changes where artwork and carousel collages are cached. The default root is `/media/fat` on MiSTer, `/tmp/mistervision-cache` in the Ghostty harness, and the user’s cache directory (usually `~/.cache`) for direct desktop runs. To keep the Ghostty cache across reboots, set `MISTERVISION_CACHE_ROOT="$HOME/.cache"` before launching the harness. The client stores caches under `mistervision` within that directory. See [artwork caching](docs/GO_BROWSING.md#persistent-artwork-cache) for details.

## Local development and testing

I use the Ghostty harness on Linux to develop and test the interface without MiSTer hardware. It also helps verify that the architecture supports different display pipelines while reusing the same UI and application logic. From the repository directory, run the browsing demo:

```bash
python3 tools/ghostty/ghostty_harness.py --demo --ntsc
```

The harness builds the client automatically. The demo uses mock data and does not play media. See the [development harness guide](tools/ghostty/README.md) for dependencies, copyable Jellyfin discovery and Plex connection commands, separate configuration profiles, and playback inside Ghostty.

## Server support and limits

Jellyfin supports remote control from other Jellyfin clients. Plex supports local DVR Live TV with alternate audio when the stream provides it. Both providers share playback controls, music visuals, picture modes, captions, and the photo viewer.

Search, automatic photo slideshows, and photo zoom are not implemented. Plex relay connections, remote control, multi-file movies, and free online TV are not supported. See [Plex details](docs/GO_PLEX.md) and [current limits](docs/README.md#current-limits).

## Deferred work

- **Broader controller support:** Testing more controllers, recognizing controller families, and showing their button labels automatically are potential future improvements. Other controllers may need a custom input profile today.
- **PAL 288p/576i and direct MiSTer YPbPr validation:** Someone with suitable hardware will need to test these output paths. I do not have that hardware. My tested setup uses MiSTer configured for RGB through its 9-pin output and a Retrovision YPbPr cable to a consumer 4:3 CRT.
- **Zaparoo DDR integration:** Deferred until I have a way to test it. Zaparoo is not required for the supported interlaced output.
- **MiSTer background-music hardware validation:** Suspension and restoration are implemented and covered by automated tests. Testing with the actual add-on is deferred because I do not use it. This is separate from music played through Jellyfin or Plex.

## More information

See the [documentation index](docs/README.md) for all guides and current limits.

- [Browsing, Continue Watching, and artwork caches](docs/GO_BROWSING.md)
- [Playback and controls](docs/GO_PLAYBACK.md)
- [Subtitles, audio tracks, and picture modes](docs/GO_PLAYBACK.md#video-options)
- [Builds and tests](docs/GO_BUILD.md)
- [Rendering architecture](docs/GO_RENDERING.md)

## Origins and license

I started this project as MiSTerFin CRT, a Go port of [MiSTerFin](https://github.com/puddingstudio/MiSTerFin) by Pudding Studio, including my [C changes](https://github.com/trentnix/MiSTerFin). I maintain it independently. It remains heavily based on MiSTerFin, an excellent project.

Original MiSTerFin material is copyright © 2026 Pudding Studio. My additions and modifications are copyright © 2026 trentnix. I distribute the application under [CC BY-NC 4.0](LICENSE), except for components covered by [separate licenses](docs/THIRD_PARTY.md), including the GPL-licensed MPlayer.
