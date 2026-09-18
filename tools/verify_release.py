"""Verify downloaded release archives and exercise the updater in temporary storage.

By default, download draft or public assets with the authenticated gh CLI. Use
--directory for assets already downloaded. Optional MiSTer checks execute only
in a unique /tmp directory and never deploy to the installed application.
"""

import argparse
import hashlib
import io
import os
from pathlib import Path, PurePosixPath
import re
import shlex
import stat
import subprocess
import tarfile
import tempfile
import zipfile

ROOT = Path(__file__).resolve().parents[1]
VERSION = r'v(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)'
MAX_EXPANDED = 512 << 20


def require(condition, message):
    if not condition:
        raise ValueError(message)


def safe_name(name):
    """Reject paths that could escape an archive or manifest namespace."""
    path = PurePosixPath(name)
    return bool(name) and not path.is_absolute() and '..' not in path.parts and '\\' not in name and '\x00' not in name and str(path) == name


def checksums(data):
    """Parse a complete checksum manifest, rejecting duplicate or unsafe names."""
    result = {}
    for line in data.decode('utf-8').splitlines():
        match = re.fullmatch(r'([0-9a-f]{64})  (.+)', line)
        require(match is not None, 'malformed checksum manifest')
        digest, name = match.groups()
        require(safe_name(name) and name not in result, 'unsafe or duplicate checksum path')
        result[name] = digest
    require(result, 'empty checksum manifest')
    return result


def digest(data):
    return hashlib.sha256(data).hexdigest()


def tar_contents(archive):
    """Hash bounded source entries without extracting files or following links."""
    contents, size = {}, 0
    for entry in archive:
        name = entry.name.rstrip('/')
        require(safe_name(name) and name not in contents, 'unsafe or duplicate source entry')
        require(entry.isfile() or entry.isdir() or entry.issym(), 'unsupported source entry')
        size += entry.size
        require(size <= MAX_EXPANDED and len(contents) < 10000, 'source archive exceeds limits')
        hashed = digest(archive.extractfile(entry).read()) if entry.isfile() else ''
        contents[name] = (entry.type, entry.mode, entry.linkname, hashed)
    return contents


def verify(directory, version, revision, repo=ROOT):
    """Verify both archives against checksums and source at the expected Git revision."""
    require(re.fullmatch(VERSION, version), 'invalid release version')
    names = {f'mistervision-{version}-mister.zip', f'mistervision-{version}-source.tar.gz'}
    outer = checksums((directory / 'SHA256SUMS').read_bytes())
    require(set(outer) == names, 'release must contain checksums for exactly the two archives')
    for name, expected in outer.items():
        path = directory / name
        require(path.stat().st_size <= MAX_EXPANDED, 'archive exceeds size limit')
        with path.open('rb') as source:
            require(hashlib.file_digest(source, 'sha256').hexdigest() == expected, f'checksum mismatch: {name}')
    with zipfile.ZipFile(directory / f'mistervision-{version}-mister.zip') as archive:
        entries = archive.infolist()
        require(len(entries) <= 128 and sum(e.file_size for e in entries) <= 128 << 20, 'ZIP exceeds limits')
        require(len({e.filename for e in entries}) == len(entries), 'duplicate ZIP entry')
        for entry in entries:
            require(safe_name(entry.filename) and stat.S_ISREG(entry.external_attr >> 16), 'unsafe ZIP entry')
        sums = checksums(archive.read('SHA256SUMS'))
        require(set(sums) == set(archive.namelist()) - {'SHA256SUMS'}, 'incomplete ZIP checksums')
        for name, expected in sums.items():
            require(digest(archive.read(name)) == expected, f'bundled checksum mismatch: {name}')
        require(archive.read('mistervision/VERSION') == (version + '\n').encode(), 'version mismatch')
        require(archive.read('mistervision/UPDATE_FORMAT') == b'1\n', 'unsupported updater format')
        build = archive.read('mistervision/BUILD.txt').decode()
        require(build.startswith(f'Version: {version}\nRevision: {revision}\n'), 'build revision mismatch')
        for name in ('mistervision/mistervision', 'mistervision/mplayer-arm'):
            data = archive.read(name)
            require(len(data) >= 52 and data[:6] == b'\x7fELF\x01\x01'
                    and data[18:20] == b'\x28\x00' and data[16:18] in (b'\x02\x00', b'\x03\x00'), 'invalid ARM executable')
        for name in ('mistervision/mistervision', 'mistervision/mplayer-arm', 'Scripts/MiSTerVision.sh'):
            require((archive.getinfo(name).external_attr >> 16) & 0o777 == 0o755, 'missing executable permissions')
    prefix = f'mistervision-{version}/'
    committed = subprocess.check_output(['git', 'archive', '--format=tar', f'--prefix={prefix}', revision], cwd=repo)
    with tarfile.open(fileobj=io.BytesIO(committed)) as archive:
        expected = tar_contents(archive)
    with tarfile.open(directory / f'mistervision-{version}-source.tar.gz', 'r:gz') as archive:
        actual = tar_contents(archive)
    upstream = actual.pop(prefix + 'docker/MPlayer-source.tar.xz', None)
    recipe = subprocess.check_output(['git', 'show', f'{revision}:docker/build-mplayer.sh'], cwd=repo).decode()
    match = re.search(r'^MPLAYER_SHA256=([0-9a-f]{64})$', recipe, re.M)
    require(match is not None and upstream is not None and upstream[0] == tarfile.REGTYPE
            and upstream[3] == match[1], 'upstream player source checksum mismatch')
    require(actual == expected, 'source archive differs from expected Git revision')
    print(f'Archive checksums, contents, ARM headers, version, and source revision verified: {revision}', flush=True)


def device_smoke(directory, version, host, identity=None):
    """Run the verified pair on MiSTer with headless video and null audio output."""
    require(re.fullmatch(r'[A-Za-z0-9_][A-Za-z0-9_.@:-]*', host), 'invalid SSH destination')
    options = ['-o', 'BatchMode=yes', '-o', 'ConnectTimeout=8']
    if identity:
        options += ['-i', str(identity)]
    ssh = ['ssh', *options, host]
    remote = subprocess.check_output(ssh + ['mktemp -d /tmp/mistervision-verify.XXXXXXXX'], text=True, timeout=15).strip()
    require(re.fullmatch(r'/tmp/mistervision-verify\.[A-Za-z0-9]+', remote), 'unexpected temporary directory')
    try:
        with tempfile.TemporaryDirectory() as local:
            local = Path(local)
            with zipfile.ZipFile(directory / f'mistervision-{version}-mister.zip') as archive:
                for name in ('mistervision', 'mplayer-arm'):
                    (local / name).write_bytes(archive.read('mistervision/' + name))
            subprocess.run(['ffmpeg', '-v', 'error', '-f', 'lavfi', '-i', 'testsrc2=s=320x240:r=30',
                            '-f', 'lavfi', '-i', 'sine=frequency=440:sample_rate=48000', '-t', '2',
                            '-c:v', 'mpeg2video', '-c:a', 'mp2', str(local / 'smoke.mpg')], check=True, timeout=30)
            subprocess.run(['scp', '-O', *options, str(local / 'mistervision'), str(local / 'mplayer-arm'),
                            str(local / 'smoke.mpg'), f'{host}:{remote}/'], check=True, timeout=90)
            script = f'''set -eu
cd {shlex.quote(remote)}
chmod 700 mistervision mplayer-arm
printf '{{}}\\n' > settings.json
./mistervision -headless 320x240 -output frame.raw -settings "$PWD/settings.json" -config "$PWD/jellyfin.conf" -state-dir "$PWD/state"
test "$(wc -c < frame.raw)" -eq 307200
./mplayer-arm -noconfig all -noconsolecontrols -nojoystick -vo null -ao null -benchmark -frames 60 smoke.mpg > player.log 2>&1
grep -q 'VIDEO:' player.log
grep -q 'Exiting... (End of file)' player.log
sha256sum mistervision mplayer-arm
'''
            subprocess.run(ssh + ['sh -s'], input=script, text=True, check=True, timeout=60)
    finally:
        subprocess.run(ssh + ['rm -rf -- ' + shlex.quote(remote)], check=True, timeout=15)
    print('MiSTer: packaged client rendered a frame and packaged player decoded 60 frames. No installed files changed.')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('version')
    parser.add_argument('--revision', help='expected commit; defaults to the local version tag')
    parser.add_argument('--directory', type=Path, help='verify existing downloaded assets instead of downloading')
    parser.add_argument('--mister', help='optional SSH destination, for example root@192.168.1.42')
    parser.add_argument('--identity', type=Path, help='SSH private key path for the optional hardware check')
    args = parser.parse_args()
    if not re.fullmatch(VERSION, args.version):
        parser.error('version must be vMAJOR.MINOR.PATCH')
    revision = subprocess.check_output(['git', 'rev-parse', '--verify', '--end-of-options',
                                       (args.revision or args.version) + '^{commit}'], cwd=ROOT, text=True).strip()
    try:
        with tempfile.TemporaryDirectory(prefix='mistervision-release-') as temporary:
            directory = args.directory.resolve() if args.directory else Path(temporary)
            if args.directory is None:
                subprocess.run(['gh', 'release', 'download', args.version, '--repo', 'trentnix/mistervision',
                                '--dir', str(directory), '--pattern', 'SHA256SUMS', '--pattern',
                                f'mistervision-{args.version}-mister.zip', '--pattern',
                                f'mistervision-{args.version}-source.tar.gz'], check=True, timeout=180)
            verify(directory, args.version, revision)
            env = dict(os.environ, MISTERVISION_VERIFY_ARCHIVE=str(directory / f'mistervision-{args.version}-mister.zip'))
            subprocess.run([os.environ.get('GO', 'go'), 'test', './internal/mister/update', '-run',
                            '^TestDownloadedRelease$', '-count=1', '-timeout=3m', '-v'], cwd=ROOT, env=env, check=True, timeout=240)
            if args.mister:
                device_smoke(directory, args.version, args.mister, args.identity)
    except (OSError, ValueError, subprocess.SubprocessError, zipfile.BadZipFile, tarfile.TarError, KeyError) as error:
        parser.exit(1, f'Release verification failed: {error}\n')


if __name__ == '__main__':
    main()
