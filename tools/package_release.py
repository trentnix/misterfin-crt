"""Build a matched MiSTer release and source bundle from a clean Git checkout."""

import argparse
import gzip
import hashlib
import io
import os
from pathlib import Path
import re
import subprocess
import tarfile
import tempfile
import time
import zipfile

ROOT = Path(__file__).resolve().parents[1]
VERSION_PATTERN = r"v(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)"


def git(root, *args):
    """Read Git metadata without including ignored configuration or build files."""
    return subprocess.check_output(["git", "-C", str(root), *args])


def checkout(root, version):
    """Reject ambiguous versions and source that cannot be reproduced from Git."""
    if re.fullmatch(VERSION_PATTERN, version) is None:
        raise ValueError("release version must be vMAJOR.MINOR.PATCH")
    if git(root, "status", "--porcelain", "--untracked-files=all").strip():
        raise ValueError("release builds require a clean checkout, including untracked files")
    return git(root, "rev-parse", "HEAD").decode().strip()


def sha256(data):
    return hashlib.sha256(data).hexdigest()


def arm_executable(path):
    """Read a little-endian ARM ELF executable, rejecting host or missing builds."""
    data = path.read_bytes()
    if len(data) < 52 or data[:6] != b"\x7fELF\x01\x01" or data[18:20] != b"\x28\x00":
        raise ValueError(f"not a 32-bit little-endian ARM executable: {path.name}")
    if int.from_bytes(data[16:18], "little") not in (2, 3):
        raise ValueError(f"not an executable ELF file: {path.name}")
    return data


def player_source(root):
    """Verify the exported upstream archive and read its unmodified license texts."""
    build = root / "build"
    script = (root / "docker/build-mplayer.sh").read_text()
    version_match = re.search(r"^MPLAYER_VER=([0-9.]+)$", script, re.M)
    checksum_match = re.search(r"^MPLAYER_SHA256=([0-9a-f]{64})$", script, re.M)
    if version_match is None or checksum_match is None:
        raise ValueError("MPlayer build recipe must declare its version and source checksum")
    version, expected = version_match.group(1), checksum_match.group(1)
    archive = (build / "misterfin-crt-mplayer-source.tar.xz").read_bytes()
    if sha256(archive) != expected:
        raise ValueError("MPlayer source archive checksum does not match the build recipe")
    licenses = {}
    with tarfile.open(fileobj=io.BytesIO(archive), mode="r:xz") as source:
        for name in ("LICENSE", "Copyright", "ffmpeg/COPYING.GPLv2", "ffmpeg/COPYING.GPLv3", "ffmpeg/COPYING.LGPLv2.1", "ffmpeg/COPYING.LGPLv3"):
            member = source.getmember(f"MPlayer-{version}/{name}")
            if not member.isfile():
                raise ValueError(f"MPlayer license is not a regular file: {name}")
            licenses[f"misterfin-crt/licenses/mplayer/{name}"] = source.extractfile(member).read()
    return archive, licenses


def source_bundle(root, path, version, epoch, upstream):
    """Include committed project source and the exact upstream player archive."""
    prefix = f"misterfin-crt-{version}/"
    committed = git(root, "archive", "--format=tar", f"--prefix={prefix}", "HEAD")
    with path.open("wb") as raw, gzip.GzipFile(filename="", mode="wb", fileobj=raw, mtime=epoch) as compressed:
        with tarfile.open(fileobj=compressed, mode="w") as output:
            with tarfile.open(fileobj=io.BytesIO(committed)) as source:
                for member in source:
                    output.addfile(member, source.extractfile(member) if member.isfile() else None)
            member = tarfile.TarInfo(prefix + "docker/MPlayer-source.tar.xz")
            member.size = len(upstream)
            member.mtime = epoch
            member.mode = 0o644
            output.addfile(member, io.BytesIO(upstream))


def write_bundle(root, version, revision, output):
    """Package freshly built files without copying active configuration or state."""
    epoch = int(git(root, "show", "-s", "--format=%ct", "HEAD"))
    upstream, licenses = player_source(root)
    payload = {
        "Scripts/MiSTerFin-CRT.sh": (root / "tools/misterfin-crt.sh").read_bytes(),
        "misterfin-crt/misterfin-crt": arm_executable(root / "build/misterfin-crt-arm"),
        "misterfin-crt/mplayer-arm": arm_executable(root / "build/misterfin-crt-mplayer-arm"),
        "misterfin-crt/jellyfin.conf.example": (root / "jellyfin.conf.example").read_bytes(),
        "misterfin-crt/settings.example.json": (root / "settings.example.json").read_bytes(),
        "misterfin-crt/LICENSE": (root / "LICENSE").read_bytes(),
        "misterfin-crt/licenses/go-LICENSE": (root / "build/go-LICENSE").read_bytes(),
        "misterfin-crt/licenses/fusion-pixel.txt": (root / "docs/licenses/fusion-pixel.txt").read_bytes(),
        "misterfin-crt/licenses/noto.txt": (root / "docs/licenses/noto.txt").read_bytes(),
        "misterfin-crt/licenses/go-text.txt": (root / "docs/licenses/go-text.txt").read_bytes(),
        "misterfin-crt/licenses/go-extensions.txt": (root / "docs/licenses/go-extensions.txt").read_bytes(),
        "misterfin-crt/licenses/coder-websocket.txt": (root / "docs/licenses/coder-websocket.txt").read_bytes(),
        "misterfin-crt/VERSION": (version + "\n").encode(),
        "misterfin-crt/UPDATE_FORMAT": b"1\n",
        "INSTALL.txt": (root / "tools/release-install.txt").read_bytes(),
        **licenses,
    }
    # Keep notices readable outside the source checkout. Relative source links
    # point to the same revision, also provided in the accompanying source bundle.
    notices = (root / "docs/THIRD_PARTY.md").read_text()

    def source_link(match):
        label, target = match.groups()
        if "://" in target:
            return match.group(0)
        relative = os.path.normpath("docs/" + target)
        return f"[{label}](https://github.com/trentnix/misterfin-crt/blob/{revision}/{relative})"
    payload["misterfin-crt/THIRD_PARTY.md"] = re.sub(r"\[([^\]]+)\]\(([^)]+)\)", source_link, notices).encode()
    metadata = (root / "build/release-manifest.txt").read_bytes()
    payload["misterfin-crt/BUILD.txt"] = f"Version: {version}\nRevision: {revision}\n\n".encode() + metadata
    payload["SHA256SUMS"] = "".join(f"{sha256(data)}  {name}\n" for name, data in sorted(payload.items())).encode()
    executable = {"Scripts/MiSTerFin-CRT.sh", "misterfin-crt/misterfin-crt", "misterfin-crt/mplayer-arm"}
    zip_path = output / f"misterfin-crt-{version}-mister.zip"
    # ZIP timestamps begin in 1980. Fix timestamps and modes so packaging the
    # same inputs does not change the archive checksum.
    stamp = time.gmtime(max(epoch, 315532800))[:6]
    with zipfile.ZipFile(zip_path, "w", compression=zipfile.ZIP_DEFLATED, compresslevel=9) as archive:
        for name, data in sorted(payload.items()):
            info = zipfile.ZipInfo(name, stamp)
            info.create_system = 3
            info.external_attr = (0o100755 if name in executable else 0o100644) << 16
            info.compress_type = zipfile.ZIP_DEFLATED
            archive.writestr(info, data)
    source_path = output / f"misterfin-crt-{version}-source.tar.gz"
    source_bundle(root, source_path, version, epoch, upstream)
    (output / "SHA256SUMS").write_text("".join(f"{sha256(path.read_bytes())}  {path.name}\n" for path in (zip_path, source_path)))


def build_release(root, version, go):
    """Build both executables from one checkout, then publish a complete local bundle."""
    revision = checkout(root, version)
    destination = root / "build/releases" / version
    if destination.exists():
        raise ValueError(f"release output already exists: {destination}")
    for target in ("arm", "native-player", "release-manifest"):
        subprocess.check_call(["make", target, f"VERSION={version}", f"GO={go}"], cwd=root)
    if checkout(root, version) != revision:
        raise ValueError("source revision changed during the release build")
    destination.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix=".release-", dir=destination.parent) as temporary:
        write_bundle(root, version, revision, Path(temporary))
        if checkout(root, version) != revision:
            raise ValueError("source revision changed during packaging")
        os.rename(temporary, destination)
    print(f"Release files: {destination}")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--version", required=True, help="stable version, such as v1.0.0")
    parser.add_argument("--go", default="go", help="Go compiler executable")
    args = parser.parse_args()
    try:
        build_release(ROOT, args.version, args.go)
    except (OSError, ValueError, subprocess.CalledProcessError, tarfile.TarError) as error:
        parser.exit(1, f"Release build failed: {error}\n")


if __name__ == "__main__":
    main()
