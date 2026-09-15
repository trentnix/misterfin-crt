# About and release availability

Press START/Menu on a controller or F1 on a keyboard while browsing to open About. The same button toggles the page closed. Back also returns to the preceding screen without changing its selection. About stays unavailable during video, music, photos, and media startup. Controller profiles can remap the `about` action. The carousel shows an About badge using the active device’s binding.

The page uses the embedded project logo, the MiSTerFin CRT name, installed version, and brief attribution and license notices. Pudding Studio is credited for original MiSTerFin material, and Trent Nix for changes. The application uses CC BY-NC 4.0, with separate terms for components such as MPlayer. See [LICENSE](../LICENSE) and [third-party notices](THIRD_PARTY.md).

## Versions

Normal development builds show `dev`, the short Git revision, and `modified` when the compiler records uncommitted changes. Builds outside a Git checkout can omit the revision. The logo is included in the executable, so installation does not require a separate artwork file.

For a release build, supply a stable semantic version:

```sh
make host VERSION=v1.0.0
make arm VERSION=v1.0.0
```

The Makefile sets `internal/release.Version` through Go linker flags. The version describes the Go client. It does not identify the separately installed MPlayer executable.

## Release checks

The application checks `trentnix/misterfin-crt` once per launch, independently of Jellyfin authentication. The request runs in the background with a timeout. Closing About lets the check finish. View/Tab checks again after the preceding request completes. Keyboard R also works. A detected release adds an update notice to the carousel.

The checker uses GitHub’s [latest-release endpoint](https://docs.github.com/en/rest/releases/releases#get-the-latest-release), excludes drafts and prereleases, and compares stable `vMAJOR.MINOR.PATCH` tags numerically. A development build can show a published release as available without claiming the release is newer than its source checkout. Identical or older stable releases do not produce an update offer.

Checks are unauthenticated and send no Jellyfin credentials. GitHub returns 404 both when no latest release exists and when a private repository is inaccessible. About displays "No public release available." for that response. It displays "Could not check for updates." for network errors, rate limits, and invalid responses. These states do not mean the installed client is up to date. This private repository needs publicly accessible release metadata before ordinary installations can discover updates. No GitHub token is read from the device.

If a release is available, B/Enter selects Update and displays "Not implemented yet." for two seconds before restoring the release status. Background checks do not replace the notice during that interval. The page stays open. This action does not download, install, restart, or modify any files. Release notes, release packaging, and actual update installation remain future work.

## Implementation and validation

`internal/release` identifies builds and reads release metadata. The command supplies that dependency to the browser. The browser owns About navigation and applies asynchronous results on its event loop. `AboutPresentation` supplies a copied snapshot to the shared raster renderer, so MiSTer and Ghostty use the same page. Static artwork and attribution are cached at the current display geometry.

Tests cover version ordering, development builds, private/missing releases, malformed and oversized responses, cancellation, input isolation, controller and terminal bindings, the placeholder action, and rendering at 640×240 and 640×288. Update-state tests use a local HTTP fixture rather than the live GitHub service.
