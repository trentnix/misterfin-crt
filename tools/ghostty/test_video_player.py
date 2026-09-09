"""Decode generated media through libmpv without a window or audio device."""

import ctypes.util
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import time
import unittest

HELPER = Path(__file__).with_name("video_player.py")
# Match the Go player's inherited media descriptor, using a non-seekable pipe.
BOOTSTRAP = "import os,runpy,sys; os.dup2(0,3); sys.argv=sys.argv[1:]; runpy.run_path(sys.argv[0],run_name='__main__')"


@unittest.skipUnless(ctypes.util.find_library("mpv") and shutil.which("ffmpeg"), "libmpv and FFmpeg required")
class VideoTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.clip = subprocess.check_output([
            "ffmpeg", "-v", "error", "-f", "lavfi", "-i", "color=c=blue:s=320x180:r=25",
            "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000", "-t", "3",
            "-c:v", "mpeg2video", "-c:a", "mp3", "-f", "mpegts", "pipe:1"])

    def start(self, directory, height):
        output = Path(directory) / "frame.raw"
        process = subprocess.Popen([sys.executable, "-c", BOOTSTRAP, str(HELPER),
                                    "--output", str(output), "--height", str(height), "--audio", "null"],
                                   stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        self.addCleanup(self.reap, process)
        return process, output

    @staticmethod
    def reap(process):
        if process.poll() is None:
            process.kill()
        process.communicate()

    def test_decode_and_letterbox_pal_and_ntsc(self):
        for height in (240, 288):
            with self.subTest(height=height), tempfile.TemporaryDirectory() as directory:
                process, output = self.start(directory, height)
                start = time.monotonic()
                stdout, stderr = process.communicate(self.clip, timeout=10)
                self.assertEqual(process.returncode, 0, stderr)
                self.assertGreater(time.monotonic() - start, 2)
                positions = [float(line.split(b"=")[1]) for line in stdout.splitlines()]
                self.assertGreater(len(positions), 5)
                self.assertLess(positions[0], 1)
                self.assertGreater(positions[-1], 2.5)
                frame = output.read_bytes()
                self.assertEqual(len(frame), 640 * height * 4)
                def pixel(y):
                    offset = (y * 640 + 320) * 4
                    return frame[offset:offset + 3]
                self.assertLess(max(pixel(height // 16)), 10)
                self.assertGreater(pixel(height // 2)[0], 200)
                self.assertLess(max(pixel(height // 2)[1:]), 20)
                self.assertLess(max(pixel(height * 15 // 16)), 10)

    def test_stop_during_playback(self):
        with tempfile.TemporaryDirectory() as directory:
            process, _ = self.start(directory, 240)
            process.stdin.write(self.clip)
            process.stdin.close()
            process.stdin = None
            self.assertTrue(process.stdout.readline().startswith(b"ANS_TIME_POSITION="))
            process.terminate()
            process.wait(timeout=2)
            self.assertEqual(process.returncode, 0)

    def test_stop_during_empty_stream(self):
        with tempfile.TemporaryDirectory() as directory:
            process, _ = self.start(directory, 240)
            time.sleep(0.4)
            process.terminate()
            process.wait(timeout=2)
            self.assertEqual(process.returncode, 0)

    def test_invalid_media_fails_without_positions(self):
        with tempfile.TemporaryDirectory() as directory:
            process, _ = self.start(directory, 240)
            stdout, _ = process.communicate(b"not a video", timeout=5)
            self.assertNotEqual(process.returncode, 0)
            self.assertEqual(stdout, b"")


if __name__ == "__main__":
    unittest.main()
