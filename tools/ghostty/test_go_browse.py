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
        if self._testMethodName == "test_select_restarts_resumable_video":
            mock.ITEMS["movie-tricky-0"]["UserData"]["PlaybackPositionTicks"] = 600000000
        if self._testMethodName == "test_music_advances_and_preserves_last_track":
            mock.CHILDREN["artist-000-album0"] = mock.CHILDREN["artist-000-album0"][:2]
        if self._testMethodName == "test_combined_continue_watching":
            for item_id in ("movie-tricky-0", "series-000-s1e01"):
                mock.ITEMS[item_id]["UserData"]["PlaybackPositionTicks"] = 600000000
                mock.ITEMS[item_id]["UserData"]["LastPlayedDate"] = "2026-09-12T12:00:00Z"
        self.movie_ids = mock.CHILDREN["view-movies"]
        self.requests = []
        self.reports = []
        self.delay_items = False
        self.home_gate = threading.Event()
        if self._testMethodName != "test_slow_continue_watching_does_not_block_libraries":
            self.home_gate.set()
        self.addCleanup(self.home_gate.set)
        self.video_response_gate = None
        test = self

        class Handler(mock.Handler):
            def log_message(self, *_args):
                pass

            def do_GET(self):
                test.requests.append(self.path)
                path = urlparse(self.path).path
                query = parse_qs(urlparse(self.path).query)
                if path == "/Items" and query.get("SortBy") == ["Random"]:
                    ids = ["artist-000-album0-t01", "artist-001-album0-t01"]
                    return self._send(self._query_result(ids, query))
                if path in ("/UserItems/Resume", "/Shows/NextUp"):
                    test.home_gate.wait(timeout=5)
                    if test._testMethodName != "test_combined_continue_watching":
                        return self._send({"Items": [], "TotalRecordCount": 0})
                    if path == "/UserItems/Resume":
                        ids = ["movie-tricky-0", "series-000-s1e01"]
                    else:
                        ids = ["series-000-s1e02", "series-001-s1e01"]
                    return self._send(self._query_result(ids, parse_qs(urlparse(self.path).query)))
                if urlparse(self.path).path.startswith(("/Videos/", "/Audio/")):
                    if (test._testMethodName == "test_select_restarts_resumable_video" and
                            parse_qs(urlparse(self.path).query).get("startTimeTicks") == ["920000000"]):
                        time.sleep(16)  # Exceed the former media response-header timeout.
                    gate = test.video_response_gate
                    if gate is not None:
                        gate.wait(timeout=3)
                    try:
                        self.send_response(200)
                        self.send_header("Content-Length", "10")
                        self.end_headers()
                        self.wfile.write(b"test video")
                    except (BrokenPipeError, ConnectionResetError):
                        pass
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
                        "TranscodingUrl": f"/Videos/{urlparse(self.path).path.split('/')[2]}/stream.ts?LiveStreamId=live-tuner"}]}).encode()
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
        (self.directory / "music.json").write_text(json.dumps({"default":"Off", "meters":False, "backgrounds":[{"name":"Off","type":"none"}]}))
        config = self.directory / "jellyfin.conf"
        config.write_text(f"http://127.0.0.1:{self.server.server_port}\nmock-api-key\nmockuser\n")
        self.frame = self.directory / "frame.raw"
        player = self.directory / "test-player"
        player.write_text("#!/bin/sh\nprintf 'ANS_TIME_POSITION=2\\n'\nsleep 30\n")
        if self._testMethodName == "test_music_advances_and_preserves_last_track":
            player.write_text("#!/bin/sh\ncat /dev/fd/3 >/dev/null\nprintf 'ANS_TIME_POSITION=3\\n'\n")
        player.chmod(0o700)
        player_args = ["-player", str(player)]
        if self._testMethodName == "test_inline_playback_owns_frame_until_stop":
            player.write_text("import argparse, pathlib, time\n"
                              "p=argparse.ArgumentParser()\n"
                              "p.add_argument('--controls',action='store_true')\n"
                              "p.add_argument('--status',action='store_true')\n"
                              "for name in ('output','width','height'): p.add_argument('--'+name)\n"
                              "a=p.parse_args()\n"
                              "pathlib.Path(a.output).write_bytes(bytes([23])*640*240*4)\n"
                              "print('ANS_TIME_POSITION=2',flush=True)\n"
                              "time.sleep(30)\n")
            player_args = ["-terminal-player", str(player)]
        if self._testMethodName == "test_video_loading_and_buffering_animation":
            player.write_text("import argparse, pathlib, time\n"
                              "p=argparse.ArgumentParser()\n"
                              "for name in ('controls','status'): p.add_argument('--'+name,action='store_true')\n"
                              "for name in ('output','width','height'): p.add_argument('--'+name)\n"
                              "a=p.parse_args()\n"
                              "stage=pathlib.Path(a.output).parent/'stage'\n"
                              "for n in range(1,4):\n"
                              " while not stage.exists() or stage.read_text()!=str(n): time.sleep(.02)\n"
                              " pathlib.Path(a.output).write_bytes(bytes([23])*640*240*4)\n"
                              " print('ANS_BUFFERING='+('true' if n==2 else 'false'),flush=True)\n"
                              " print('ANS_TIME_POSITION='+str(n+1),flush=True)\n"
                              "time.sleep(30)\n")
            player_args = ["-terminal-player", str(player)]
        master, slave = pty.openpty()
        self.master = master
        self.addCleanup(os.close, master)
        self.log = (self.directory / "browser.log").open("w+b")
        self.addCleanup(self.log.close)

        controlling_terminal = self._testMethodName != "test_terminal_stdin_without_controlling_terminal"

        def terminal_session():
            os.setsid()
            if controlling_terminal:
                fcntl.ioctl(0, termios.TIOCSCTTY, 0)

        self.process = subprocess.Popen(
            [str(BINARY), "-browse", "-headless", "640x240", "-output", str(self.frame),
             "-config", str(config), "-state-dir", str(self.directory / "state")] + player_args,
            stdin=slave, stdout=self.log, stderr=self.log, preexec_fn=terminal_session,
            env={**os.environ, "MISTERFIN_CACHE_ROOT": str(self.directory / "cache")},
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

    def read_frame(self):
        # The raw framebuffer writer truncates before writing. Like the
        # presenter, wait for a complete frame that stayed stable during read.
        deadline = time.monotonic() + 2
        while time.monotonic() < deadline:
            before = self.frame.stat()
            data = self.frame.read_bytes()
            after = self.frame.stat()
            if (len(data) == 640 * 240 * 4 and before.st_size == after.st_size == len(data)
                    and before.st_mtime_ns == after.st_mtime_ns):
                return data
            time.sleep(0.005)
        self.fail("no complete framebuffer frame published")

    def key(self, key):
        os.write(self.master, key)

    def test_slow_continue_watching_does_not_block_libraries(self):
        self.assertFalse(self.home_gate.is_set())
        self.key(b"b")
        self.wait_request("/Items", ParentId="view-movies", StartIndex=0)
        self.home_gate.set()
        self.key(b"b")
        self.wait_request("/Items/movie-tricky-0")

    def test_combined_continue_watching(self):
        self.wait_request("/UserItems/Resume", MediaTypes="Video")
        self.wait_request("/Shows/NextUp", enableResumable="false")
        # The combined card is first. Left also handles arrival after libraries.
        self.key(b"\x1b[D")
        time.sleep(0.15)
        self.key(b"b")
        time.sleep(0.25)
        self.key(b"b")
        self.wait_request("/Items/movie-tricky-0")
        self.key(b"b")
        self.wait_request("/Videos/movie-tricky-0/stream", startTimeTicks=600000000)
        self.key(b"a")
        time.sleep(0.3)
        self.key(b"a")
        time.sleep(0.2)
        self.key(b"\x1b[Bb")
        self.wait_request("/Items/series-000-s1e01")
        self.key(b"a")
        time.sleep(0.2)
        self.key(b"\x1b[Bb")
        self.wait_request("/Items/series-001-s1e01")
        self.assertFalse(any("misterfin-go%3Acontinue" in request for request in self.requests))
        self.assertFalse(any(urlparse(request).path == "/Items/series-000-s1e02" for request in self.requests))

    def test_terminal_stdin_without_controlling_terminal(self):
        # Scripts launch can pass terminal stdin without a controlling terminal.
        self.key(b"b")
        self.wait_request("/Items", ParentId="view-movies", StartIndex=0)
        self.assertEqual(len(self.read_frame()), 640 * 240 * 4)
        self.key(b"q")
        self.assertEqual(self.process.wait(timeout=3), 0)

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
        self.assertEqual(len(self.read_frame()), 640 * 240 * 4)
        self.key(b"q")
        self.assertEqual(self.process.wait(timeout=3), 0)

    def test_list_prefetches_before_boundary_and_reuses_previous_page(self):
        self.key(b"b")
        self.wait_request("/Items", ParentId="view-movies", StartIndex=0)
        # Seven six-row jumps reach item 42, before the first page boundary.
        for _ in range(7):
            self.key(b"\x1b[C")
            time.sleep(0.06)
        self.wait_request("/Items", ParentId="view-movies", StartIndex=64)
        for _ in range(4):
            self.key(b"\x1b[C")
            time.sleep(0.06)
        # Item 66 -> item 60 must use the retained previous page.
        self.key(b"\x1b[D")
        time.sleep(0.1)
        self.key(b"b")
        self.wait_request("/Items/" + self.movie_ids[60])
        pages = []
        for request in self.requests:
            parsed = urlparse(request)
            query = parse_qs(parsed.query)
            if (parsed.path == "/Items" and query.get("ParentId") == ["view-movies"]
                    and query.get("Limit") == ["64"]):
                pages.append(query.get("StartIndex"))
        self.assertEqual(pages, [["0"], ["64"]])

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
            self.assertEqual(self.read_frame(), bytes([23]) * 640 * 240 * 4)
            self.key(b"\x1b[A")
            time.sleep(0.1)
            self.assertNotEqual(self.read_frame(), bytes([23]) * 640 * 240 * 4)
            self.key(b"b")
            deadline = time.monotonic() + 5
            while not any(path == "/Sessions/Playing/Progress" and body.get("IsPaused") for path, body in self.reports):
                self.assertLess(time.monotonic(), deadline, "video pause was not reported")
                time.sleep(0.02)
            self.assertEqual(self.read_frame(), bytes([23]) * 640 * 240 * 4)
            self.key(b"b")
            time.sleep(0.1)
            self.assertEqual(self.read_frame(), bytes([23]) * 640 * 240 * 4)
        self.key(b"a")
        while not any(path == "/Sessions/Playing/Stopped" for path, _ in self.reports):
            self.assertLess(time.monotonic(), deadline, "no playback stop")
            time.sleep(0.02)
        time.sleep(0.3)
        self.assertIsNone(self.process.poll())
        if self._testMethodName == "test_inline_playback_owns_frame_until_stop":
            self.assertNotEqual(self.read_frame(), bytes([23]) * 640 * 240 * 4)
        starts = [body for path, body in self.reports if path == "/Sessions/Playing"]
        stops = [body for path, body in self.reports if path == "/Sessions/Playing/Stopped"]
        self.assertEqual(starts[0]["PlaySessionId"], stops[0]["PlaySessionId"])
        self.assertEqual(stops[0]["PositionTicks"], 20000000)
        # Playback returns to details. Back returns to Movies, then to home.
        self.key(b"aa")
        time.sleep(0.1)
        self.key(b"\x1b[Cb")
        self.wait_request("/Items", ParentId="view-tv", StartIndex=0)

    def test_select_restarts_resumable_video(self):
        self.key(b"b")
        self.wait_request("/Items", ParentId="view-movies", StartIndex=0)
        self.key(b"b")
        self.wait_request("/Items/movie-tricky-0")
        self.key(b"b")
        self.wait_request("/Videos/movie-tricky-0/stream", startTimeTicks=600000000)
        self.key(b"a")
        deadline = time.monotonic() + 5
        while not any(path == "/Sessions/Playing/Stopped" for path, _ in self.reports):
            self.assertLess(time.monotonic(), deadline, "resume playback did not stop")
            time.sleep(0.02)
        time.sleep(0.3)
        self.key(b"\t")
        self.wait_request("/Videos/movie-tricky-0/stream", startTimeTicks=0)
        deadline = time.monotonic() + 5
        while not any(path == "/Sessions/Playing" and body.get("PositionTicks") == 20000000
                      for path, body in self.reports):
            self.assertLess(time.monotonic(), deadline, "restart retained the saved resume offset")
            time.sleep(0.02)
        self.key(b"lll")
        self.wait_request("/Videos/movie-tricky-0/stream", startTimeTicks=920000000)
        deadline = time.monotonic() + 22
        while not any(path == "/Sessions/Playing" and body.get("PositionTicks") == 940000000
                      for path, body in self.reports):
            self.assertLess(time.monotonic(), deadline, "seek after restart did not reach destination")
            time.sleep(0.02)
        self.key(b"a")

    def test_video_seek_accumulates_and_preserves_pause(self):
        self.key(b"b")
        self.wait_request("/Items", ParentId="view-movies", StartIndex=0)
        self.key(b"b")
        self.wait_request("/Items/movie-tricky-0")
        self.key(b"b")
        self.wait_request("/Videos/movie-tricky-0/stream", startTimeTicks=0)
        initial_stream = next(r for r in self.requests
                              if urlparse(r).path == "/Videos/movie-tricky-0/stream")
        initial_session = parse_qs(urlparse(initial_stream).query)["playSessionId"][0]
        self.key(b"ll")
        deadline = time.monotonic() + 5
        while not any(path == "/Sessions/Playing/Progress" and body.get("IsPaused") and
                      body.get("PlaySessionId") == initial_session for path, body in self.reports):
            self.assertLess(time.monotonic(), deadline, "seek did not pause the old decoder")
            time.sleep(0.02)
        self.wait_request("/Videos/movie-tricky-0/stream", startTimeTicks=620000000)
        streams = [r for r in self.requests if urlparse(r).path == "/Videos/movie-tricky-0/stream"]
        self.assertEqual(len(streams), 2, "rapid presses restarted separately")
        self.assertNotEqual(parse_qs(urlparse(streams[0]).query)["playSessionId"],
                            parse_qs(urlparse(streams[1]).query)["playSessionId"])
        self.key(b"b")
        time.sleep(0.2)
        self.assertTrue(any(path == "/Sessions/Playing/Progress" and body.get("IsPaused")
                            for path, body in self.reports))
        self.reports.clear()
        self.key(b"j")
        self.wait_request("/Videos/movie-tricky-0/stream", startTimeTicks=340000000)
        deadline = time.monotonic() + 5
        while not any(path == "/Sessions/Playing/Progress" and body.get("IsPaused")
                      for path, body in self.reports):
            self.assertLess(time.monotonic(), deadline, "seek did not restore pause")
            time.sleep(0.02)
        # Back cancels a pending seek without starting another stream.
        self.key(b"la")
        time.sleep(0.9)
        streams = [r for r in self.requests if urlparse(r).path == "/Videos/movie-tricky-0/stream"]
        self.assertEqual(len(streams), 3)
        self.assertIsNone(self.process.poll())

    def test_video_seek_retargets_while_replacement_is_loading(self):
        self.key(b"b")
        self.wait_request("/Items", ParentId="view-movies", StartIndex=0)
        self.key(b"b")
        self.wait_request("/Items/movie-tricky-0")
        self.key(b"b")
        self.wait_request("/Videos/movie-tricky-0/stream", startTimeTicks=0)

        gate = threading.Event()
        self.video_response_gate = gate
        self.addCleanup(gate.set)
        self.key(b"ll")
        self.wait_request("/Videos/movie-tricky-0/stream", startTimeTicks=620000000)

        self.key(b"l")
        time.sleep(0.2)
        targets = [int(parse_qs(urlparse(r).query).get("startTimeTicks", ["0"])[0])
                   for r in self.requests
                   if urlparse(r).path == "/Videos/movie-tricky-0/stream"]
        self.assertNotIn(920000000, targets, "retarget skipped the destination-time delay")
        self.wait_request("/Videos/movie-tricky-0/stream", startTimeTicks=920000000)
        self.key(b"j")
        time.sleep(0.2)
        targets = [int(parse_qs(urlparse(r).query).get("startTimeTicks", ["0"])[0])
                   for r in self.requests
                   if urlparse(r).path == "/Videos/movie-tricky-0/stream"]
        self.assertEqual(targets.count(620000000), 1, "left retarget skipped the destination-time delay")
        deadline = time.monotonic() + 5
        while True:
            targets = [int(parse_qs(urlparse(r).query).get("startTimeTicks", ["0"])[0])
                       for r in self.requests
                       if urlparse(r).path == "/Videos/movie-tricky-0/stream"]
            if targets.count(620000000) >= 2:
                break
            self.assertLess(time.monotonic(), deadline, f"seek did not retarget left: {targets}")
            time.sleep(0.01)
        self.assertEqual(targets[-3:], [620000000, 920000000, 620000000])
        gate.set()
        time.sleep(0.3)
        self.key(b"a")
        self.assertIsNone(self.process.poll())

    def test_video_loading_and_buffering_animation(self):
        self.key(b"b")
        self.wait_request("/Items", ParentId="view-movies", StartIndex=0)
        self.key(b"b")
        self.wait_request("/Items/movie-tricky-0")
        self.key(b"b")
        self.wait_request("/Videos/movie-tricky-0/stream")
        first = self.read_frame()
        self.assertTrue(any(first), "loading screen was blank")
        time.sleep(0.2)
        self.assertNotEqual(first, self.read_frame(), "loading indicator did not animate")
        clean = bytes([23]) * 640 * 240 * 4
        for stage in (1, 2, 3):
            (self.directory / "stage").write_text(str(stage))
            time.sleep(0.3)
            if stage == 2:
                first = self.read_frame()
                self.assertNotEqual(first, clean, "buffering was not shown")
                time.sleep(0.2)
                self.assertNotEqual(first, self.read_frame(), "buffering did not animate")
            else:
                self.assertEqual(self.read_frame(), clean, "indicator remained during playback")
        self.key(b"a")

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
        time.sleep(0.3)
        # One press retunes the selected channel from the restored list.
        self.requests.clear()
        self.key(b"b")
        self.wait_request("/Videos/channel-2-1/stream.ts")
        self.key(b"a")
        time.sleep(0.4)
        # Down selects the next channel, and one confirm starts it.
        self.key(b"\x1b[Bb")
        self.wait_request("/Videos/channel-5-1/stream.ts")
        self.key(b"a")
        time.sleep(0.4)
        # Back from the restored channel list returns directly to libraries.
        self.key(b"a")
        time.sleep(0.1)
        self.key(b"\x1b[Db")
        self.wait_request("/Items", ParentId="view-music")

    def test_photo_opens_full_screen_and_returns_to_folder(self):
        self.key(b"\x1b[C\x1b[C\x1b[C\x1b[Cb")
        self.wait_request("/Items", ParentId="view-homevideos")
        self.key(b"\x1b[Bb")
        self.wait_request("/Items/photo-landscape/Images/Primary", quality=90, maxWidth=640, maxHeight=240)
        frame = self.read_frame()
        offset = (120 * 640 + 320) * 4
        self.assertEqual(frame[offset:offset + 3], bytes([215, 125, 35]))
        self.key(b"\x1b[C")
        self.wait_request("/Items/photo-portrait/Images/Primary", quality=90)
        self.key(b"\x1b[D")
        time.sleep(0.15)  # The previous photo is already cached.
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
        self.wait_request("/Audio/artist-000-album0-t01/stream", static="true")
        self.assertEqual(len(self.read_frame()), 640 * 240 * 4)
        self.key(b"\x1b[A")  # Reveal only. Do not change tracks.
        time.sleep(0.1)
        self.assertEqual(sum(urlparse(r).path.startswith("/Audio/") for r in self.requests), 1)
        self.key(b"b")  # Pause and hide the instructions.
        deadline = time.monotonic() + 5
        while not any(path == "/Sessions/Playing/Progress" and body.get("IsPaused") for path, body in self.reports):
            self.assertLess(time.monotonic(), deadline, "pause was not reported")
            time.sleep(0.02)
        time.sleep(0.1)
        self.assertEqual(self.read_frame()[220 * 640 * 4:], bytes(20 * 640 * 4))
        self.key(b"b")
        time.sleep(0.1)
        self.key(b"]")  # Hidden controls must not consume navigation.
        self.wait_request("/Audio/artist-000-album0-t02/stream", static="true")
        self.key(b"]")  # The next move must also take one press.
        self.wait_request("/Audio/artist-000-album0-t03/stream", static="true")
        self.key(b"[")
        deadline = time.monotonic() + 5
        while sum(urlparse(r).path == "/Audio/artist-000-album0-t02/stream" for r in self.requests) < 2:
            self.assertLess(time.monotonic(), deadline, "Left bracket did not change tracks immediately")
            time.sleep(0.02)
        self.key(b"a")
        deadline = time.monotonic() + 5
        while not any(path == "/Sessions/Playing/Stopped" for path, _ in self.reports):
            self.assertLess(time.monotonic(), deadline, "music did not stop")
            time.sleep(0.02)
        playing = [body for path, body in self.reports if path == "/Sessions/Playing"]
        self.assertEqual(playing[0]["PlayMethod"], "DirectStream")
        self.assertIsNone(self.process.poll())

    def test_whole_library_shuffle_and_return(self):
        self.key(b"\x1b[C\x1b[Cb")
        self.wait_request("/Items", ParentId="view-music")
        self.key(b"\x1b[B")  # Preserve the second artist while shuffling.
        self.key(b"\t")
        self.wait_request("/Items", ParentId="view-music", SortBy="Random", IncludeItemTypes="Audio")
        self.wait_request("/Audio/artist-000-album0-t01/stream")
        self.key(b"]")
        self.wait_request("/Audio/artist-001-album0-t01/stream")
        self.key(b"[")
        deadline = time.monotonic() + 5
        while sum(urlparse(r).path == "/Audio/artist-000-album0-t01/stream" for r in self.requests) < 2:
            self.assertLess(time.monotonic(), deadline, "shuffle previous did not return to the played track")
            time.sleep(.02)
        self.key(b"a")
        time.sleep(.2)
        self.key(b"b")
        self.wait_request("/Items", ParentId="artist-001")
        self.assertIsNone(self.process.poll())

    def test_music_advances_and_preserves_last_track(self):
        self.key(b"\x1b[C\x1b[Cb")
        self.wait_request("/Items", ParentId="view-music")
        self.key(b"b")
        self.wait_request("/Items", ParentId="artist-000")
        self.key(b"b")
        self.wait_request("/Items", ParentId="artist-000-album0")
        self.key(b"b")
        self.wait_request("/Audio/artist-000-album0-t01/stream")
        self.wait_request("/Audio/artist-000-album0-t02/stream")
        deadline = time.monotonic() + 5
        while sum(path == "/Sessions/Playing/Stopped" for path, _ in self.reports) < 2:
            self.assertLess(time.monotonic(), deadline, "queue did not finish")
            time.sleep(0.02)
        time.sleep(0.2)
        self.key(b"b")  # Back in the album with the last track selected.
        while sum(urlparse(r).path == "/Audio/artist-000-album0-t02/stream" for r in self.requests) < 2:
            self.assertLess(time.monotonic(), deadline, "last-track selection was not restored")
            time.sleep(0.02)
        self.assertFalse(any("t03/stream" in r for r in self.requests))

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
