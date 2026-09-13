"""Decode generated media through libmpv without a window or audio device."""

import ctypes.util
from pathlib import Path
import shutil
import select
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
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

    def start(self, directory, height, status=False, zoom=False):
        output = Path(directory) / "frame.raw"
        process = subprocess.Popen([sys.executable, "-c", BOOTSTRAP, str(HELPER),
                                    "--output", str(output), "--height", str(height), "--audio", "null"] + (["--status"] if status else []) + (["--zoom-4-3"] if zoom else []),
                                   stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        self.addCleanup(self.reap, process)
        return process, output

    @staticmethod
    def reap(process):
        if process.poll() is None:
            process.kill()
        process.communicate()

    def test_reports_buffering_separately_from_position(self):
        with tempfile.TemporaryDirectory() as directory:
            process, _ = self.start(directory, 240, status=True)
            stdout, stderr = process.communicate(self.clip, timeout=10)
            self.assertEqual(process.returncode, 0, stderr)
            lines = stdout.splitlines()
            self.assertIn(b"ANS_BUFFERING=false", lines)
            self.assertTrue(any(line.startswith(b"ANS_TIME_POSITION=") for line in lines))
            self.assertTrue(all(line.startswith(b"ANS_TIME_POSITION=") or line in
                                (b"ANS_BUFFERING=true", b"ANS_BUFFERING=false") for line in lines))

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

    def test_zoom_removes_baked_side_bars_without_stretching(self):
        # A 4:3 blue picture inside a 16:9 file. Original retains all four bars.
        # Zoom must remove them while preserving the centered red square's shape.
        clip = subprocess.check_output([
            "ffmpeg", "-v", "error", "-f", "lavfi", "-i",
            "color=c=blue:s=240x180:r=25,drawbox=x=90:y=60:w=60:h=60:color=red:t=fill,pad=320:180:40:0:black",
            "-t", "1", "-c:v", "mpeg2video", "-f", "mpegts", "pipe:1"])
        for height in (240, 288):
            for zoom in (False, True):
                with self.subTest(height=height, zoom=zoom), tempfile.TemporaryDirectory() as directory:
                    process, output = self.start(directory, height, zoom=zoom)
                    _, stderr = process.communicate(clip, timeout=10)
                    self.assertEqual(process.returncode, 0, stderr)
                    frame = output.read_bytes()
                    def pixel(x, y):
                        offset = (y * 640 + x) * 4
                        return frame[offset:offset + 3]
                    for x, y in ((16, height // 2), (320, height // 16)):
                        if zoom:
                            self.assertGreater(pixel(x, y)[0], 200)
                        else:
                            self.assertLess(max(pixel(x, y)), 10)
                    red_width = sum(pixel(x, height // 2)[2] > 180 for x in range(640))
                    red_height = sum(pixel(320, y)[2] > 180 for y in range(height))
                    # Logical rows become tall CRT pixels on the 4:3 screen.
                    self.assertAlmostEqual(red_width / (red_height * 480 / height), 1, delta=0.04)

    def test_audio_only_keeps_browser_frame(self):
        clip = subprocess.check_output(["ffmpeg", "-v", "error", "-f", "lavfi", "-i",
                                        "sine=frequency=440:sample_rate=44100", "-t", "2",
                                        "-c:a", "flac", "-f", "flac", "pipe:1"])
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "frame.raw"
            output.write_bytes(b"browser frame")
            result = subprocess.run([sys.executable, "-c", BOOTSTRAP, str(HELPER),
                                     "--audio-only", "--audio", "null", "--output", str(output)],
                                    input=clip, capture_output=True, timeout=8)
            self.assertEqual(result.returncode, 0, result.stderr)
            positions = [float(line.split(b"=")[1]) for line in result.stdout.splitlines()]
            self.assertGreater(len(positions), 3)
            self.assertGreater(positions[-1], 1.0)
            self.assertEqual(output.read_bytes(), b"browser frame")

    def test_audio_levels_measure_stereo_signal(self):
        clip = subprocess.check_output(["ffmpeg", "-v", "error", "-f", "lavfi", "-i",
                                       "aevalsrc=0.5*sin(440*2*PI*t)|0.1*sin(880*2*PI*t):s=48000",
                                       "-t", "2", "-f", "wav", "pipe:1"])
        result = subprocess.run([sys.executable, "-c", BOOTSTRAP, str(HELPER),
                                 "--audio-only", "--audio", "null", "--audio-levels"],
                                input=clip, capture_output=True, timeout=8)
        self.assertEqual(result.returncode, 0, result.stderr)
        levels = [tuple(map(float, line.split(b"=")[1].split(b",")))
                  for line in result.stdout.splitlines() if line.startswith(b"ANS_AUDIO_LEVELS=")]
        active = [(left, right) for left, right in levels if left > 0.1]
        self.assertGreater(len(active), 5)
        for left, right in active:
            self.assertAlmostEqual(left, 0.3535, delta=0.03)
            self.assertAlmostEqual(right, 0.0707, delta=0.015)

    def test_audio_pause_resume(self):
        clip = subprocess.check_output(["ffmpeg", "-v", "error", "-f", "lavfi", "-i",
                                        "sine=frequency=440:sample_rate=44100", "-t", "8",
                                        "-c:a", "pcm_s16le", "-f", "wav", "pipe:1"])
        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *_args):
                pass
            def do_GET(self):
                start = int(self.headers.get("Range", "bytes=0-").split("=")[1].split("-")[0])
                self.send_response(206 if "Range" in self.headers else 200)
                self.send_header("Content-Length", str(len(clip) - start))
                self.send_header("Accept-Ranges", "bytes")
                self.send_header("Content-Type", "audio/wav")
                if "Range" in self.headers:
                    self.send_header("Content-Range", f"bytes {start}-{len(clip)-1}/{len(clip)}")
                self.end_headers()
                try:
                    self.wfile.write(clip[start:])
                except (BrokenPipeError, ConnectionResetError):
                    pass
        server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        worker = threading.Thread(target=server.serve_forever, daemon=True)
        worker.start()
        self.addCleanup(server.server_close)
        self.addCleanup(worker.join)
        self.addCleanup(server.shutdown)
        process = subprocess.Popen([sys.executable, str(HELPER), "--audio-only", "--audio", "null",
                                    "--source", f"http://127.0.0.1:{server.server_port}/audio"],
                                   stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, bufsize=0)
        self.addCleanup(self.reap, process)
        def position():
            self.assertTrue(select.select([process.stdout], [], [], 3)[0], "no position feedback")
            line = process.stdout.readline()
            self.assertTrue(line.startswith(b"ANS_TIME_POSITION="), line)
            return float(line.split(b"=")[1])
        position()
        process.stdin.write(b"pause true\n")
        position()  # Allow an already queued report to drain.
        paused = position()
        self.assertAlmostEqual(position(), paused, delta=0.05)
        process.stdin.write(b"pause false\n")
        deadline = time.monotonic() + 3
        while position() < paused + 0.2:
            self.assertLess(time.monotonic(), deadline, "audio did not resume")
        process.terminate()
        process.wait(timeout=2)
        self.assertEqual(process.returncode, 0)

    def test_video_pause_resume_with_separate_control_pipe(self):
        clip = subprocess.check_output(["ffmpeg", "-v", "error", "-f", "lavfi", "-i",
                                        "testsrc2=size=320x180:rate=25", "-f", "lavfi", "-i",
                                        "sine=frequency=440:sample_rate=48000", "-t", "4",
                                        "-c:v", "mpeg2video", "-c:a", "mp3", "-f", "mpegts", "pipe:1"])
        with tempfile.TemporaryDirectory() as directory:
            media = Path(directory) / "clip.ts"
            media.write_bytes(clip)
            output = Path(directory) / "frame.raw"
            bootstrap = "import os,sys,runpy; fd=os.open(sys.argv[1],os.O_RDONLY); os.dup2(fd,3); sys.argv=sys.argv[2:]; runpy.run_path(sys.argv[0],run_name='__main__')"
            process = subprocess.Popen([sys.executable, "-c", bootstrap, str(media), str(HELPER),
                                        "--controls", "--audio", "null", "--output", str(output)],
                                       stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, bufsize=0)
            self.addCleanup(self.reap, process)
            self.assertTrue(select.select([process.stdout], [], [], 3)[0])
            self.assertTrue(process.stdout.readline().startswith(b"ANS_TIME_POSITION="))
            process.stdin.write(b"pause true\n")
            time.sleep(0.3)
            paused = output.read_bytes()
            time.sleep(0.4)
            self.assertEqual(output.read_bytes(), paused)
            process.stdin.write(b"pause false\n")
            time.sleep(0.4)
            self.assertNotEqual(output.read_bytes(), paused)
            process.terminate()
            process.wait(timeout=2)
            self.assertEqual(process.returncode, 0)

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
