"""Check release contents, source provenance, and failure behavior without cross-compiling."""

import hashlib
import io
from pathlib import Path
import subprocess
import tarfile
import tempfile
import unittest
from unittest import mock
import zipfile

from tools import package_release as release


class ReleaseTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.write(".gitignore", b"build/\njellyfin.conf\nsettings.json\nstate/\n")
        for name in ("tools/misterfin-crt.sh", "tools/release-install.txt", "jellyfin.conf.example", "settings.example.json", "LICENSE", "docs/licenses/coder-websocket.txt", "docs/licenses/fusion-pixel.txt", "docs/licenses/noto.txt", "docs/licenses/go-text.txt", "docs/licenses/go-extensions.txt"):
            self.write(name, name.encode())
        self.write("docs/THIRD_PARTY.md", b"[license](../LICENSE) [external](https://example.org)\n")
        archive = io.BytesIO()
        with tarfile.open(fileobj=archive, mode="w:xz") as source:
            for name in ("LICENSE", "Copyright", "ffmpeg/COPYING.GPLv2", "ffmpeg/COPYING.GPLv3", "ffmpeg/COPYING.LGPLv2.1", "ffmpeg/COPYING.LGPLv3"):
                data = name.encode()
                member = tarfile.TarInfo("MPlayer-1.5/" + name)
                member.size = len(data)
                source.addfile(member, io.BytesIO(data))
        self.upstream = archive.getvalue()
        self.write("docker/build-mplayer.sh", f"MPLAYER_VER=1.5\nMPLAYER_SHA256={release.sha256(self.upstream)}\n".encode())
        self.write("build/misterfin-crt-mplayer-source.tar.xz", self.upstream)
        elf = bytearray(52)
        elf[:6] = b"\x7fELF\x01\x01"
        elf[16:20] = b"\x02\x00\x28\x00"
        for name in ("misterfin-crt-arm", "misterfin-crt-mplayer-arm"):
            self.write("build/" + name, elf)
        self.write("build/release-manifest.txt", b"paired build metadata\n")
        self.write("build/go-LICENSE", b"Go license")
        self.write("jellyfin.conf", b"private token")
        self.write("settings.json", b"private settings")
        self.write("state/session.json", b"private sign-in")
        self.run_git("init", "-q")
        self.run_git("add", ".")
        self.run_git("-c", "user.name=Release Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "Release fixture")
        self.revision = release.checkout(self.root, "v0.1.0")
        self.output = self.root / "build/releases/v0.1.0"
        self.output.mkdir(parents=True)

    def write(self, name, data):
        path = self.root / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(data)

    def run_git(self, *args):
        subprocess.run(["git", "-C", str(self.root), *args], check=True, capture_output=True)

    def package(self):
        release.write_bundle(self.root, "v0.1.0", self.revision, self.output)

    def test_payload_preserves_settings_and_has_correct_hashes_and_modes(self):
        self.package()
        with zipfile.ZipFile(self.output / "misterfin-crt-v0.1.0-mister.zip") as archive:
            names = archive.namelist()
            self.assertIn("misterfin-crt/jellyfin.conf.example", names)
            self.assertIn("misterfin-crt/settings.example.json", names)
            self.assertIn("misterfin-crt/licenses/mplayer/LICENSE", names)
            self.assertEqual(archive.read("misterfin-crt/licenses/fusion-pixel.txt"), b"docs/licenses/fusion-pixel.txt")
            for name in ("noto", "go-text", "go-extensions"):
                self.assertEqual(archive.read(f"misterfin-crt/licenses/{name}.txt"), f"docs/licenses/{name}.txt".encode())
            self.assertFalse(any("state/" in name or name.endswith(("/settings.json", "/jellyfin.conf")) for name in names))
            self.assertEqual(archive.read("misterfin-crt/VERSION"), b"v0.1.0\n")
            self.assertEqual(archive.read("misterfin-crt/UPDATE_FORMAT"), b"1\n")
            for line in archive.read("SHA256SUMS").decode().splitlines():
                digest, name = line.split("  ", 1)
                self.assertEqual(digest, hashlib.sha256(archive.read(name)).hexdigest())
            for name in ("Scripts/MiSTerFin-CRT.sh", "misterfin-crt/misterfin-crt", "misterfin-crt/mplayer-arm"):
                self.assertEqual(archive.getinfo(name).external_attr >> 16, 0o100755)
            notices = archive.read("misterfin-crt/THIRD_PARTY.md").decode()
            self.assertIn(f"/blob/{self.revision}/LICENSE", notices)
            self.assertIn("https://example.org", notices)
        for line in (self.output / "SHA256SUMS").read_text().splitlines():
            digest, name = line.split("  ", 1)
            self.assertEqual(digest, release.sha256((self.output / name).read_bytes()))

    def test_source_contains_exact_upstream_and_committed_recipe_without_secrets(self):
        self.package()
        with tarfile.open(self.output / "misterfin-crt-v0.1.0-source.tar.gz") as archive:
            prefix = "misterfin-crt-v0.1.0/"
            self.assertEqual(archive.extractfile(prefix + "docker/MPlayer-source.tar.xz").read(), self.upstream)
            self.assertIn(prefix + "docker/build-mplayer.sh", archive.getnames())
            self.assertNotIn(prefix + "jellyfin.conf", archive.getnames())
            self.assertNotIn(prefix + "state/session.json", archive.getnames())

    def test_same_inputs_produce_identical_archives(self):
        self.package()
        before = {path.name: path.read_bytes() for path in self.output.iterdir()}
        self.package()
        self.assertEqual(before, {path.name: path.read_bytes() for path in self.output.iterdir()})

    def test_rejects_bad_version_and_dirty_source_before_build(self):
        for version in ("dev", "v1.2", "v01.2.3", "v1.2.3-rc1", "../v1.2.3"):
            with self.subTest(version=version), self.assertRaisesRegex(ValueError, "version"):
                release.checkout(self.root, version)
        self.write("LICENSE", b"changed")
        with self.assertRaisesRegex(ValueError, "clean checkout"):
            release.checkout(self.root, "v0.1.0")
        self.run_git("checkout", "--", "LICENSE")
        self.write("untracked.txt", b"not committed")
        with self.assertRaisesRegex(ValueError, "clean checkout"):
            release.checkout(self.root, "v0.1.0")

    def test_rejects_corrupt_source_or_wrong_architecture(self):
        self.write("build/misterfin-crt-mplayer-source.tar.xz", b"bad archive")
        with self.assertRaisesRegex(ValueError, "checksum"):
            self.package()
        self.write("build/misterfin-crt-mplayer-source.tar.xz", self.upstream)
        self.write("build/misterfin-crt-arm", b"host executable")
        with self.assertRaisesRegex(ValueError, "ARM executable"):
            self.package()

    def test_existing_release_is_not_overwritten(self):
        self.write("build/releases/v0.1.0/keep", b"existing release")
        with mock.patch.object(release.subprocess, "check_call") as run:
            with self.assertRaisesRegex(ValueError, "already exists"):
                release.build_release(self.root, "v0.1.0", "go")
            run.assert_not_called()
        self.assertEqual((self.output / "keep").read_bytes(), b"existing release")

    def test_build_failure_does_not_publish_partial_release(self):
        self.output.rmdir()
        with mock.patch.object(release.subprocess, "check_call", side_effect=subprocess.CalledProcessError(1, "make")):
            with self.assertRaises(subprocess.CalledProcessError):
                release.build_release(self.root, "v0.1.0", "go")
        self.assertFalse(self.output.exists())

    def test_packaging_failure_does_not_publish_partial_release(self):
        self.output.rmdir()
        self.write("build/misterfin-crt-mplayer-source.tar.xz", b"corrupted")
        with mock.patch.object(release.subprocess, "check_call"):
            with self.assertRaisesRegex(ValueError, "checksum"):
                release.build_release(self.root, "v0.1.0", "go")
        self.assertEqual(list(self.output.parent.iterdir()), [])

    def test_source_changes_during_build_stop_packaging(self):
        self.output.rmdir()
        def change_source(*args, **kwargs):
            self.write("LICENSE", b"changed during build")
        with mock.patch.object(release.subprocess, "check_call", side_effect=change_source):
            with self.assertRaisesRegex(ValueError, "clean checkout"):
                release.build_release(self.root, "v0.1.0", "go")
        self.assertFalse(self.output.exists())

    def test_builds_pair_before_manifest_and_packaging(self):
        self.output.rmdir()
        with mock.patch.object(release.subprocess, "check_call") as run:
            release.build_release(self.root, "v0.1.0", "custom-go")
        self.assertEqual([call.args[0][1] for call in run.call_args_list], ["arm", "native-player", "release-manifest"])
        self.assertTrue(all("VERSION=v0.1.0" in call.args[0] for call in run.call_args_list))
        self.assertTrue((self.output / "SHA256SUMS").is_file())


if __name__ == "__main__":
    unittest.main()
