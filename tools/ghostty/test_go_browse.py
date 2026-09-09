"""Integration checks for the built Go browser against the inherited mock server."""

import fcntl
import importlib.util
import os
from pathlib import Path
import pty
import subprocess
import tempfile
import termios
import threading
import time
import unittest
from http.server import HTTPServer
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
        self.requests = []
        self.delay_items = False
        test = self

        class Handler(mock.Handler):
            def log_message(self, *_args):
                pass

            def do_GET(self):
                test.requests.append(self.path)
                if test.delay_items and urlparse(self.path).path == "/Items":
                    time.sleep(0.4)
                try:
                    super().do_GET()
                except (BrokenPipeError, ConnectionResetError):
                    pass  # Expected when the browser cancels a delayed request.

        self.server = HTTPServer(("127.0.0.1", 0), Handler)
        worker = threading.Thread(target=self.server.serve_forever, daemon=True)
        worker.start()
        self.addCleanup(self.server.server_close)
        self.addCleanup(worker.join)
        self.addCleanup(self.server.shutdown)
        config = self.directory / "jellyfin.conf"
        config.write_text(f"http://127.0.0.1:{self.server.server_port}\nmock-api-key\nmockuser\n")
        self.frame = self.directory / "frame.raw"
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
             "-config", str(config), "-state-dir", str(self.directory / "state")],
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
        for offset in range(64, 449, 64):
            self.key(b"\x1b[C")
            self.wait_request("/Items", ParentId="view-movies", StartIndex=offset)
        self.key(b"a")
        time.sleep(0.1)
        self.key(b"\x1b[B\x1b[Bb")
        self.wait_request("/Items", ParentId="view-music")
        self.key(b"b")
        self.wait_request("/Items", ParentId="artist-000")
        self.key(b"b")
        self.wait_request("/Items", ParentId="artist-000-album0")
        self.assertEqual(self.frame.stat().st_size, 640 * 240 * 4)
        self.key(b"q")
        self.assertEqual(self.process.wait(timeout=3), 0)

    def test_back_cancels_delayed_library(self):
        self.delay_items = True
        self.key(b"b")
        self.wait_request("/Items", ParentId="view-movies")
        self.key(b"a")
        time.sleep(0.5)  # Give the obsolete request time to finish.
        self.key(b"\x1b[Bb")
        self.wait_request("/Items", ParentId="view-tv")
        time.sleep(0.4)
        self.key(b"b")
        self.wait_request("/Shows/series-000/Seasons")


if __name__ == "__main__":
    unittest.main()
