"""Reject corrupt or mismatched release artifacts before installation or execution."""

import hashlib
import io
import tarfile
import unittest

from tools import test_package_release
from tools.verify_release import checksums, verify


class VerificationTests(unittest.TestCase):
    def setUp(self):
        self.fixture = test_package_release.ReleaseTest()
        self.fixture.setUp()
        self.addCleanup(self.fixture.doCleanups)
        self.fixture.package()

    def test_packaged_pair_and_source_match_revision(self):
        f = self.fixture
        verify(f.output, 'v0.1.0', f.revision, f.root)

    def test_corrupt_asset_fails(self):
        f = self.fixture
        with (f.output / 'mistervision-v0.1.0-mister.zip').open('ab') as archive:
            archive.write(b'corrupted')
        with self.assertRaisesRegex(ValueError, 'checksum mismatch'):
            verify(f.output, 'v0.1.0', f.revision, f.root)

    def test_rehashed_source_from_another_revision_is_rejected(self):
        f = self.fixture
        path = f.output / 'mistervision-v0.1.0-source.tar.gz'
        with tarfile.open(path) as archive:
            entries = [(entry, archive.extractfile(entry).read() if entry.isfile() else None)
                       for entry in archive]
        with tarfile.open(path, 'w:gz') as archive:
            for entry, data in entries:
                if entry.name == 'mistervision-v0.1.0/LICENSE':
                    data = b'changed license'
                    entry.size = len(data)
                archive.addfile(entry, io.BytesIO(data) if data is not None else None)
        manifest = f.output / 'SHA256SUMS'
        manifest.write_text(''.join(hashlib.sha256((f.output / name).read_bytes()).hexdigest() + '  ' + name + '\n'
                                    for name in checksums(manifest.read_bytes())))
        with self.assertRaisesRegex(ValueError, 'source archive differs'):
            verify(f.output, 'v0.1.0', f.revision, f.root)

    def test_wrong_revision_fails_before_install(self):
        f = self.fixture
        with self.assertRaisesRegex(ValueError, 'revision mismatch'):
            verify(f.output, 'v0.1.0', '0' * 40, f.root)

    def test_manifest_rejects_traversal_duplicates_and_omissions(self):
        for data in (b'', b'not a checksum', (('0' * 64) + '  ../escape\n').encode(),
                     ((('0' * 64) + '  file\n') * 2).encode()):
            with self.subTest(data=data), self.assertRaises(ValueError):
                checksums(data)
