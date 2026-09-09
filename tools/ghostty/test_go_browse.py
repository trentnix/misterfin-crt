"""Integration checks for the built Go browser against the inherited mock server."""

import fcntl
import importlib.util
import json
import os
from pathlib import Path
import pty
import subprocess
import tempfile
import termios
import threading
import time
import unittest
from http.server import ThreadingHTTPServer
from urllib.parse import urlparse, parse_qs

ROOT = Path(__file__).resolve().parents[2]
BINARY = Path(os.environ.get("MISTERFIN_GO_TEST_BINARY", str(ROOT / "build/misterfin-go")))


@unittest.skipUnless(BINARY.is_file(), "build the Go host binary first")
class BrowseIntegrationTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.directory = Path(self.temp.name)
        spec = importlib.util.spec_from_file_location("mock_jellyfin", ROOT / "tools/mock-jellyfin.py")
        mock = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(mock)
        mock.ITEMS.update({channel["Id"]: channel for channel in mock.LIVE_CHANNELS})
        self.requests = []
        self.reports = []
        self.delay_items = False
        test = self

        class Handler(mock.Handler):
            def log_message(self, *_args):
                pass

            def do_GET(self):
                test.requests.append(self.path)
                if urlparse(self.path).path.startswith(("/Videos/", "/Audio/")):
                    self.send_response(200)
                    self.send_header("Content-Length", "10")
                    self.end_headers()
                    self.wfile.write(b"test video")
                    return
                if test.delay_items and urlparse(self.path).path == "/Items":
                    time.sleep(0.4)
                try:
                    super().do_GET()
                except (BrokenPipeError, ConnectionResetError):
                    pass  # Expected when the browser cancels a delayed request.

            def do_POST(self):
                length = int(self.headers.get("Content-Length", "0"))
                body = json.loads(self.rfile.read(length)) if length else {}
                test.reports.append((urlparse(self.path).path, body))
                if urlparse(self.path).path.endswith("/PlaybackInfo"):
                    payload = json.dumps({"PlaySessionId": "live-session", "MediaSources": [{
                        "Id": "live-source", "LiveStreamId": "live-tuner",
                        "TranscodingUrl": "/Videos/channel-2-1/stream.ts?LiveStreamId=live-tuner"}]}).encode()
                    self.send_response(200)
                    self.send_header("Content-Length", str(len(payload)))
                    self.end_headers()
                    self.wfile.write(payload)
                    return
                self.send_response(204)
                self.end_headers()

        self.server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        worker = threading.Thread(target=self.server.serve_forever, daemon=True)
        worker.start()
        self.addCleanup(self.server.server_close)
        self.addCleanup(worker.join)
        self.addCleanup(self.server.shutdown)
        config = self.directory / "jellyfin.conf"
        config.write_text(f"http://127.0.0.1:{self.server.server_port}\nmock-api-key\nmockuser\n")
        self.frame = self.directory / "frame.raw"
        player = self.directory / "test-player"
        player.write_text("#!/bin/sh\nprintf 'ANS_TIME_POSITION=2\\n'\nsleep 30\n")
        player.chmod(0o700)
        player_args = ["-player", str(player)]
        if self._testMethodName == "test_inline_playback_owns_frame_until_stop":
            player.write_text("import argparse, pathlib, time\n"
                              "p=argparse.ArgumentParser()\n"
                              "for name in ('output','width','height'): p.add_argument('--'+name)\n"
                              "a=p.parse_args()\n"
                              "pathlib.Path(a.output).write_bytes(bytes([23])*640*240*4)\n"
                              "print('ANS_TIME_POSITION=2',flush=True)\n"
                              "time.sleep(30)\n")
            player_args = ["-terminal-player", str(player)]
        master, slave = pty.openpty()
        self.master = master
        self.addCleanup(os.close, master)
        self.log = (self.directory / "browser.log").open("w+b")
        self.addCleanup(self.log.close)

        def terminal_session():
            os.setsid()
            fcntl.ioctl(0, termios.TIOCSCTTY, 0)

        self.process = subprocess.Popen(
            [str(BINARY), "-browse", "-headless", "640x240", "-output", str(self.frame),
             "-config", str(config), "-state-dir", str(self.directory / "state")] + player_args,
            stdin=slave, stdout=self.log, stderr=self.log, preexec_fn=terminal_session,
        )
        os.close(slave)
        self.addCleanup(self.stop)
        self.wait_request("/UserViews")

    def stop(self):
        if self.process.poll() is None:
            self.process.terminate()
            try:
                self.process.wait(timeout=3)
            except subprocess.TimeoutExpired:
                self.process.kill()
                self.process.wait()

    def wait_request(self, path, **query):
        deadline = time.monotonic() + 5
        while time.monotonic() < deadline:
            for request in self.requests:
                parsed = urlparse(request)
                params = parse_qs(parsed.query)
                if parsed.path == path and all(params.get(k) == [str(v)] for k, v in query.items()):
                    # Allow the loopback response to reach the UI event loop.
                    time.sleep(0.15)
                    self.assertIsNone(self.process.poll())
                    return params
            if self.process.poll() is not None:
                self.log.seek(0)
                self.fail(self.log.read().decode())
            time.sleep(0.01)
        self.fail(f"request not observed: {path} {query}; got {self.requests}")

    def key(self, key):
        os.write(self.master, key)

    def test_movie_paging_and_music_hierarchy(self):
        self.key(b"b")
        self.wait_request("/Items", ParentId="view-movies", StartIndex=0)
        jumps = 0
        for offset in range(64, 449, 64):
            needed = (offset + 5) // 6
            for _ in range(needed - jumps):
                self.key(b"\x1b[C")
                time.sleep(0.06)
            self.wait_request("/Items", ParentId="view-movies", StartIndex=offset)
            jumps = needed
        self.key(b"a")
        time.sleep(0.1)
        self.key(b"\x1b[C\x1b[Cb")
        self.wait_request("/Items", ParentId="view-music")
        self.key(b"b")
        self.wait_request("/Items", ParentId="artist-000")
        self.key(b"b")
        self.wait_request("/Items", ParentId="artist-000-album0")
        self.assertEqual(self.frame.stat().st_size, 640 * 240 * 4)
        self.key(b"q")
        self.assertEqual(self.process.wait(timeout=3), 0)

    def test_playback_stop_returns_to_details(self):
        self.key(b"b")
        self.wait_request("/Items", ParentId="view-movies", StartIndex=0)
        self.key(b"b")
        self.wait_request("/Items/movie-tricky-0")
        self.key(b"b")
        self.wait_request("/Videos/movie-tricky-0/stream")
        deadline = time.monotonic() + 5
        while not any(path == "/Sessions/Playing" for path, _ in self.reports):
            self.assertLess(time.monotonic(), deadline, "no playback start")
            time.sleep(0.02)
        if self._testMethodName == "test_inline_playback_owns_frame_until_stop":
            time.sleep(0.3)
            self.assertEqual(self.frame.read_bytes(), bytes([23]) * 640 * 240 * 4)
        self.key(b"a")
        while not any(path == "/Sessions/Playing/Stopped" for path, _ in self.reports):
            self.assertLess(time.monotonic(), deadline, "no playback stop")
            time.sleep(0.02)
        time.sleep(0.3)
        self.assertIsNone(self.process.poll())
        if self._testMethodName == "test_inline_playback_owns_frame_until_stop":
            self.assertNotEqual(self.frame.read_bytes(), bytes([23]) * 640 * 240 * 4)
        starts = [body for path, body in self.reports if path == "/Sessions/Playing"]
        stops = [body for path, body in self.reports if path == "/Sessions/Playing/Stopped"]
        self.assertEqual(starts[0]["PlaySessionId"], stops[0]["PlaySessionId"])
        self.assertEqual(stops[0]["PositionTicks"], 20000000)
        # Playback returns to details. Back returns to Movies, then to home.
        self.key(b"aa")
        time.sleep(0.1)
        self.key(b"\x1b[Cb")
        self.wait_request("/Items", ParentId="view-tv", StartIndex=0)

    def test_inline_playback_owns_frame_until_stop(self):
        self.test_playback_stop_returns_to_details()

    def test_live_tv_uses_channels_endpoint(self):
        self.key(b"\x1b[C\x1b[C\x1b[Cb")
        params = self.wait_request("/LiveTv/Channels", StartIndex=0, Limit=64)
        self.assertEqual(params["AddCurrentProgram"], ["true"])
        self.assertNotIn("SortBy", params)
        self.assertFalse(any(parse_qs(urlparse(r).query).get("ParentId") == ["view-live-tv"]
                             for r in self.requests))

    def test_live_tv_playback_releases_tuner(self):
        self.key(b"\x1b[C\x1b[C\x1b[Cb")
        self.wait_request("/LiveTv/Channels", StartIndex=0)
        self.key(b"b")
        self.wait_request("/Items/channel-2-1")
        self.key(b"b")
        self.wait_request("/Videos/channel-2-1/stream.ts")
        self.key(b"a")
        deadline = time.monotonic() + 5
        while not any(path == "/LiveStreams/Close" for path, _ in self.reports):
            self.assertLess(time.monotonic(), deadline, "tuner was not released")
            time.sleep(0.02)
        self.assertFalse(any("/UserData" in path for path, _ in self.reports))
        stopped = [body for path, body in self.reports if path == "/Sessions/Playing/Stopped"]
        self.assertEqual(stopped[0]["LiveStreamId"], "live-tuner")
        self.assertFalse(stopped[0]["CanSeek"])
        self.assertIsNone(self.process.poll())

    def test_photo_opens_full_screen_and_returns_to_folder(self):
        self.key(b"\x1b[C\x1b[C\x1b[C\x1b[Cb")
        self.wait_request("/Items", ParentId="view-homevideos")
        self.key(b"\x1b[Bb")
        self.wait_request("/Items/photo-landscape/Images/Primary", quality=90, maxWidth=640, maxHeight=240)
        frame = self.frame.read_bytes()
        offset = (120 * 640 + 320) * 4
        self.assertEqual(frame[offset:offset + 3], bytes([215, 125, 35]))
        self.key(b"a")
        time.sleep(0.15)
        self.key(b"\x1b[Bb")
        self.wait_request("/Items", ParentId="photo-album")
        self.assertFalse(any(path.startswith("/Sessions/") for path, _ in self.reports))

    def test_music_plays_with_browser_frame_and_stops(self):
        self.key(b"\x1b[C\x1b[Cb")
        self.wait_request("/Items", ParentId="view-music")
        self.key(b"b")
        self.wait_request("/Items", ParentId="artist-000")
        self.key(b"b")
        self.wait_request("/Items", ParentId="artist-000-album0")
        self.key(b"b")
        self.wait_request("/Items/artist-000-album0-t01")
        details = self.frame.read_bytes()
        self.key(b"b")
        self.wait_request("/Audio/artist-000-album0-t01/stream", static="true")
        self.assertNotEqual(self.frame.read_bytes(), details)
        self.assertEqual(len(self.frame.read_bytes()), 640 * 240 * 4)
        self.key(b"a")
        deadline = time.monotonic() + 5
        while not any(path == "/Sessions/Playing/Stopped" for path, _ in self.reports):
            self.assertLess(time.monotonic(), deadline, "music did not stop")
            time.sleep(0.02)
        playing = [body for path, body in self.reports if path == "/Sessions/Playing"]
        self.assertEqual(playing[0]["PlayMethod"], "DirectStream")
        self.assertIsNone(self.process.poll())

    def test_back_cancels_delayed_library(self):
        self.delay_items = True
        self.key(b"b")
        self.wait_request("/Items", ParentId="view-movies")
        self.key(b"a")
        time.sleep(0.5)  # Give the obsolete request time to finish.
        self.key(b"\x1b[Cb")
        self.wait_request("/Items", ParentId="view-tv")
        time.sleep(0.4)
        self.key(b"b")
        self.wait_request("/Shows/series-000/Seasons")


if __name__ == "__main__":
    unittest.main()
