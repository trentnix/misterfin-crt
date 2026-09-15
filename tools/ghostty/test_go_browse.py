"""Integration checks for the built Go browser against the inherited mock server."""

import base64
import hashlib
import queue
import struct
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
BINARY = Path(os.environ.get("MISTERFIN_CRT_TEST_BINARY", str(ROOT / "build/misterfin-crt")))


@unittest.skipUnless(BINARY.is_file(), "build the Go host binary first")
class BrowseIntegrationTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.directory = Path(self.temp.name)
        # Integration fixtures must never open the workstation audio device for UI cues.
        (self.directory / "sounds.json").write_text('{"enabled": false}\n')
        spec = importlib.util.spec_from_file_location("mock_jellyfin", ROOT / "tools/mock-jellyfin.py")
        mock = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(mock)
        mock.ITEMS.update({channel["Id"]: channel for channel in mock.LIVE_CHANNELS})
        if self._testMethodName == "test_custom_browsing_background":
            (self.directory / "background.png").write_bytes(mock._png(200, 40, 20, 8, 6))
            (self.directory / "background.json").write_text('{"image":"background.png"}\n')
        if self._testMethodName == "test_missing_background_falls_back":
            (self.directory / "background.json").write_text('{"image":"missing.png"}\n')
        if self._testMethodName == "test_non_image_background_falls_back":
            (self.directory / "background.json").write_text('{"image":"not-an-image.txt"}\n')
            (self.directory / "not-an-image.txt").write_text("private non-image contents")
        if self._testMethodName == "test_select_restarts_resumable_video":
            mock.ITEMS["movie-tricky-0"]["UserData"]["PlaybackPositionTicks"] = 600000000
        if self._testMethodName == "test_music_advances_and_preserves_last_track":
            mock.CHILDREN["artist-000-album0"] = mock.CHILDREN["artist-000-album0"][:2]
        if self._testMethodName in ("test_combined_continue_watching", "test_continue_card_is_selected_before_feed_arrives"):
            for item_id in ("movie-tricky-0", "series-000-s1e01"):
                mock.ITEMS[item_id]["UserData"]["PlaybackPositionTicks"] = 600000000
                mock.ITEMS[item_id]["UserData"]["LastPlayedDate"] = "2026-09-12T12:00:00Z"
        if self._testMethodName == "test_mixed_library_movie_series_and_folder_navigation":
            mock.VIEWS = [{"Id": "view-mixed", "Name": "Nostalgia"}]
            mock.CHILDREN["view-mixed"] = ["movie-tricky-0", "series-000", "mixed-folder"]
            mock.ITEMS["mixed-folder"] = mock.base_item("mixed-folder", "More titles", "Folder")
            mock.CHILDREN["mixed-folder"] = ["movie-tricky-1"]
        self.movie_ids = mock.CHILDREN["view-movies"]
        self.remote_commands = queue.Queue()
        self.remote_done = threading.Event()
        self.addCleanup(self.remote_done.set)
        self.requests = []
        self.reports = []
        self.delay_items = False
        self.home_gate = threading.Event()
        if self._testMethodName not in ("test_slow_continue_watching_does_not_block_libraries", "test_continue_card_is_selected_before_feed_arrives"):
            self.home_gate.set()
        self.addCleanup(self.home_gate.set)
        self.video_response_gate = None
        self.stop_report_gate = threading.Event()
        self.stop_report_gate.set()
        test = self

        class Handler(mock.Handler):
            def log_message(self, *_args):
                pass

            def _send(self, payload, content_type="application/json", status=200):
                try:
                    return super()._send(payload, content_type, status)
                except (BrokenPipeError, ConnectionResetError):
                    pass  # App restart can cancel in-flight home and artwork requests.

            def do_GET(self):
                test.requests.append(self.path)
                path = urlparse(self.path).path
                query = parse_qs(urlparse(self.path).query)
                if path == "/socket" and test._testMethodName == "test_remote_playback_queue":
                    # This fixture sends server frames only. Production framing,
                    # TLS, reconnects, and heartbeat handling have Go tests.
                    key = self.headers["Sec-WebSocket-Key"]
                    accept = base64.b64encode(hashlib.sha1((key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11").encode()).digest()).decode()
                    self.send_response(101)
                    self.send_header("Upgrade", "websocket")
                    self.send_header("Connection", "Upgrade")
                    self.send_header("Sec-WebSocket-Accept", accept)
                    self.end_headers()
                    self.close_connection = True
                    while not test.remote_done.is_set():
                        try:
                            command = test.remote_commands.get(timeout=.1)
                        except queue.Empty:
                            continue
                        data = json.dumps(command).encode()
                        header = bytes([0x81, len(data)]) if len(data) < 126 else bytes([0x81, 126]) + struct.pack("!H", len(data))
                        try:
                            self.wfile.write(header + data)
                            self.wfile.flush()
                        except (BrokenPipeError, ConnectionResetError):
                            break
                    return
                if path == "/Items" and "Ids" in query:
                    return self._send(self._query_result(query["Ids"][0].split(","), query))
                if (test._testMethodName == "test_mixed_library_movie_series_and_folder_navigation"
                        and path == "/Items" and query.get("ParentId") == ["view-mixed"]
                        and "MusicArtist" in query.get("IncludeItemTypes", [""])[0].split(",")):
                    # Reproduce the unrelated artist rows returned by the real server.
                    return self._send(self._query_result(["artist-001"], query))
                if path == "/Items" and query.get("SortBy") == ["Random"]:
                    ids = ["artist-000-album0-t01", "artist-001-album0-t01"]
                    return self._send(self._query_result(ids, query))
                if path in ("/UserItems/Resume", "/Shows/NextUp"):
                    test.home_gate.wait(timeout=5)
                    if test._testMethodName not in ("test_combined_continue_watching", "test_continue_card_is_selected_before_feed_arrives"):
                        return self._send({"Items": [], "TotalRecordCount": 0})
                    if path == "/UserItems/Resume":
                        ids = ["movie-tricky-0", "series-000-s1e01"]
                    else:
                        ids = ["series-000-s1e02", "series-001-s1e01"]
                    return self._send(self._query_result(ids, parse_qs(urlparse(self.path).query)))
                if "/Subtitles/" in path:
                    payload = b"1\n00:00:00,000 --> 00:01:00,000\nShared subtitle text"
                    self.send_response(200)
                    self.send_header("Content-Length", str(len(payload)))
                    self.end_headers()
                    self.wfile.write(payload)
                    return
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
                if (test._testMethodName in (
                        "test_about_preserves_selection_and_blocks_browse_input",
                        "test_movie_paging_and_music_hierarchy",
                        "test_select_restarts_resumable_video",
                        "test_slow_continue_watching_does_not_block_libraries")
                        and (path == "/UserViews" or
                             (path == "/Items" and query.get("StartIndex") == ["0"]
                              and query.get("Limit") == ["64"]))):
                    time.sleep(.3)  # Deliberately exceed the former 150 ms guess.
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
                if urlparse(self.path).path == "/Sessions/Playing/Stopped":
                    test.stop_report_gate.wait(timeout=5)
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
        if self._testMethodName == "test_invalid_optional_settings_keep_browsing":
            (self.directory / "ui.json").write_text('{"title":42}\n')
            (self.directory / "sounds.json").write_text('{"volume":999}\n')
            (self.directory / "music.json").write_text('{"backgrounds":[]}\n')
        if self._testMethodName == "test_missing_music_asset_keeps_playback":
            (self.directory / "music.json").write_text(json.dumps({
                "default": "Custom", "meters": False,
                "backgrounds": [{"name": "Custom", "type": "image", "files": ["missing.png"]},
                                {"name": "Off", "type": "none"}],
            }))
        config = self.directory / "jellyfin.conf"
        config.write_text(f"http://127.0.0.1:{self.server.server_port}\nmock-api-key\nmockuser\n")
        if self._testMethodName == "test_transcode_profile_from_configuration":
            with config.open("a") as config_file:
                config_file.write("640x480@8000000\n")
        self.frame = self.directory / "frame.raw"
        player = self.directory / "test-player"
        player.write_text("#!/bin/sh\nprintf 'ANS_TIME_POSITION=2\\n'\nsleep 30\n")
        if self._testMethodName == "test_music_advances_and_preserves_last_track":
            player.write_text("#!/bin/sh\ncat /dev/fd/3 >/dev/null\nprintf 'ANS_TIME_POSITION=3\\n'\n")
        player.chmod(0o700)
        player_args = ["-player", str(player)]
        if self._testMethodName in ("test_inline_playback_owns_frame_until_stop", "test_video_track_selection", "test_video_picture_selection", "test_video_choices_survive_stop_and_app_restart", "test_view_back_returns_to_clean_video", "test_live_captions_toggle_without_retuning"):
            player.write_text("import argparse, pathlib, select, sys, time\n"
                              "p=argparse.ArgumentParser()\n"
                              "p.add_argument('--controls',action='store_true')\n"
                              "p.add_argument('--status',action='store_true')\n"
                              "p.add_argument('--zoom-4-3',action='store_true')\n"
                              "p.add_argument('--captions',action='store_true')\n"
                              "for name in ('output','width','height'): p.add_argument('--'+name)\n"
                              "a=p.parse_args()\n"
                              "with (pathlib.Path(a.output).parent/'picture-modes').open('a') as log: log.write(str(a.zoom_4_3)+'\\n')\n"
                              "pathlib.Path(a.output).write_bytes(bytes([23])*640*240*4)\n"
                              "print('ANS_TIME_POSITION=2',flush=True)\n"
                              "if a.captions: print('ANS_CAPTION_TEXT='+b'Live caption text'.hex(),flush=True)\n"
                              "deadline=time.monotonic()+30\n"
                              "while time.monotonic()<deadline:\n"
                              " if not select.select([sys.stdin],[],[],.05)[0]: continue\n"
                              " parts=sys.stdin.readline().split()\n"
                              " if len(parts)==3 and parts[0]=='picture':\n"
                              "  with (pathlib.Path(a.output).parent/'picture-modes').open('a') as log: log.write(str(parts[1]=='1')+'\\n')\n"
                              "  print('ANS_PICTURE_MODE='+parts[2]+','+parts[1],flush=True)\n")
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

        # Browser milestones synchronize input with applied results, rather
        # than assuming a loopback response is rendered within a fixed delay.
        self.diagnostics = self.directory / "logs" / "diagnostics.log"
        (self.directory / "diagnostics.json").write_text(json.dumps({
            "enabled": True, "path": "logs/diagnostics.log", "max_bytes": 65536,
        }))

        # Run the main suite through sectioned settings. Keep one legacy launch
        # to cover installations that have not migrated yet.
        if self._testMethodName != "test_legacy_settings_still_load":
            settings = {}
            for name in ("ui", "background", "sounds", "music", "diagnostics"):
                legacy = self.directory / f"{name}.json"
                if legacy.exists():
                    value = json.loads(legacy.read_text())
                    if name == "music":
                        if "default" in value:
                            value["default_background"] = value.pop("default")
                        if "meters" in value:
                            value["show_audio_meters"] = value.pop("meters")
                        settings["music_visuals"] = value
                    elif name == "sounds":
                        settings.setdefault("ui", {})["navigation_sounds"] = value
                    else:
                        settings[name] = value
                    legacy.unlink()
            (self.directory / "settings.json").write_text(json.dumps(settings))

        self.process = subprocess.Popen(
            [str(BINARY), "-browse", "-headless", "640x240", "-output", str(self.frame),
             "-config", str(config), "-state-dir", str(self.directory / "state")] + player_args,
            stdin=slave, stdout=self.log, stderr=self.log, preexec_fn=terminal_session,
            # Keep release checks offline. Jellyfin fixtures use loopback HTTP.
            env={**os.environ, "MISTERFIN_CACHE_ROOT": str(self.directory / "cache"), "MISTERFIN_SETTINGS": "",
                 "HTTPS_PROXY": "http://127.0.0.1:1", "NO_PROXY": "127.0.0.1,localhost"},
        )
        os.close(slave)
        self.addCleanup(self.stop)
        self.wait_request("/UserViews")
        if self.home_gate.is_set():
            self.wait_event("browser.home", failed=False)

    def stop(self):
        if self.process.poll() is None:
            self.process.terminate()
            try:
                self.process.wait(timeout=3)
            except subprocess.TimeoutExpired:
                self.process.kill()
                self.process.wait()

    def wait_request(self, path, **query):
        if path == "/Items" and "ParentId" in query and query.get("SortBy") != "Random":
            # Library counts and carousel artwork also request /Items. They
            # must not satisfy a wait for the navigable list page.
            query.setdefault("Limit", 64)
        deadline = time.monotonic() + 5
        while time.monotonic() < deadline:
            for request in self.requests:
                parsed = urlparse(request)
                params = parse_qs(parsed.query)
                if parsed.path == path and all(params.get(k) == [str(v)] for k, v in query.items()):
                    if path == "/UserViews":
                        self.wait_event("browser.page", kind="views", failed=False)
                    elif (path == "/Items" and "ParentId" in params
                          and params.get("Limit") == ["64"]):
                        self.wait_event("browser.page", parent=params["ParentId"][0],
                                        start=int(params.get("StartIndex", ["0"])[0]), failed=False)
                    else:
                        # Playback assertions also inspect decoder reports or
                        # rendered frames after observing the request.
                        time.sleep(0.15)
                    self.assertIsNone(self.process.poll())
                    return params
            if self.process.poll() is not None:
                self.log.seek(0)
                self.fail(self.log.read().decode())
            time.sleep(0.01)
        self.fail(f"request not observed: {path} {query}; got {self.requests}")

    def wait_event(self, name, **attributes):
        """Wait for an applied browser result in the optional diagnostic log."""
        deadline = time.monotonic() + 5
        while time.monotonic() < deadline:
            if self.diagnostics.exists():
                # The writer can be appending a line. Only parse complete lines.
                lines = self.diagnostics.read_bytes().split(b"\n")[:-1]
                for line in lines:
                    event = json.loads(line)
                    if event.get("msg") == name and all(event.get(k) == v for k, v in attributes.items()):
                        return event
            if self.process.poll() is not None:
                self.log.seek(0)
                self.fail(self.log.read().decode())
            time.sleep(.01)
        self.fail(f"browser event not observed: {name} {attributes}")

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

    def test_custom_browsing_background(self):
        self.wait_request("/Items", ParentId="view-movies", Limit=0)
        self.assertEqual(self.read_frame()[-4:-1], bytes((8, 17, 86)))
        self.assertFalse(any("/Images/" in request for request in self.requests))
        self.assertFalse(any(parse_qs(urlparse(request).query).get("Limit") == ["12"]
                             for request in self.requests))
        # The source is decoded once. Navigating must not reopen the image.
        (self.directory / "background.png").unlink()
        self.key(b"b")
        self.wait_request("/Items", ParentId="view-movies", StartIndex=0)
        self.wait_request("/Items/movie-tricky-0/Images/Primary")
        self.assertEqual(self.read_frame()[-4:-1], bytes((8, 17, 86)))
        self.assertFalse(any("/Images/Backdrop" in request for request in self.requests))
        self.key(b"b")
        self.wait_request("/Items/movie-tricky-0")
        self.wait_request("/Items/movie-tricky-0/Images/Backdrop/0")

    def test_missing_background_falls_back(self):
        self.wait_event("configuration.fallback", configuration="background",
                        error_kind="not-found", fallback="normal-artwork")
        self.assert_normal_artwork_and_navigation()

    def test_non_image_background_falls_back(self):
        self.wait_event("configuration.fallback", configuration="background",
                        error_kind="invalid", fallback="normal-artwork")
        self.assert_normal_artwork_and_navigation()

    def assert_normal_artwork_and_navigation(self):
        """Failed custom images must retain mosaic loading and library navigation."""
        self.wait_request("/Items", ParentId="view-movies", Limit=12)
        self.assertTrue(any("/Images/" in request for request in self.requests))
        self.key(b"b")
        self.wait_request("/Items", ParentId="view-movies", StartIndex=0)
        self.wait_request("/Items/movie-tricky-0/Images/Primary")
        self.wait_request("/Items/movie-tricky-0/Images/Backdrop/0")
        self.assertIsNone(self.process.poll())

    def test_invalid_optional_settings_keep_browsing(self):
        for configuration, fallback in (("ui", "default-title"),
                                        ("ui.navigation_sounds", "sounds-off"),
                                        ("music_visuals", "music-backgrounds-off")):
            self.wait_event("configuration.fallback", configuration=configuration,
                            error_kind="invalid", fallback=fallback)
        self.key(b"b")
        self.wait_request("/Items", ParentId="view-movies", StartIndex=0)
        self.key(b"b")
        self.wait_request("/Items/movie-tricky-0")
        self.assertIsNone(self.process.poll())
        self.key(b"aa")  # Return from details and the list to the carousel.
        self.exercise_music_playback()

    def test_missing_music_asset_keeps_playback(self):
        self.wait_event("configuration.fallback", configuration="music_visuals",
                        error_kind="not-found", fallback="selected-background-unavailable")
        self.exercise_music_playback()

    def test_legacy_settings_still_load(self):
        self.assertFalse((self.directory / "settings.json").exists())
        self.key(b"b")
        self.wait_request("/Items", ParentId="view-movies", StartIndex=0)
        self.assertTrue(self.diagnostics.exists())

    def test_diagnostics_lifecycle(self):
        self.key(b"b")
        self.wait_request("/Items", ParentId="view-movies", StartIndex=0)
        self.stop()
        path = self.directory / "logs" / "diagnostics.log"
        events = [json.loads(line) for line in path.read_text().splitlines()]
        self.assertEqual(events[0]["msg"], "application.start")
        display = next(e for e in events if e["msg"] == "application.display")
        self.assertEqual(display["output_height"], 240)
        self.assertTrue(any(e["msg"] == "application.phase" and e["stage"] == "browser" for e in events))
        self.assertTrue(any(e["msg"] == "input.backend" and e["terminal"] for e in events))
        self.assertEqual(events[-1]["msg"], "application.exit")
        self.assertFalse(events[-1]["failed"])
        self.assertTrue(any(e["msg"] == "http.request" and e["path"] == "/UserViews" for e in events))
        for event in events:
            self.assertNotIn("?", event.get("path", ""))
        self.assertLessEqual(path.stat().st_size, 65536)

    def test_about_preserves_selection_and_blocks_browse_input(self):
        self.key(b"b")
        self.wait_request("/Items", ParentId="view-movies", StartIndex=0)
        self.key(b"\x1b[B")
        time.sleep(.2)
        self.key(b"\x1bOP")  # F1 opens About through the terminal decoder.
        deadline = time.monotonic() + 3
        while time.monotonic() < deadline:
            if self.read_frame()[:3] == bytes((0x13, 0x0d, 0x0b)):
                break
            time.sleep(.02)
        else:
            self.fail("About frame did not appear")
        self.key(b"\x1b[Cb")
        time.sleep(.2)
        self.assertFalse(any(urlparse(r).path == "/Items/movie-tricky-1" for r in self.requests))
        self.key(b"\x1bOP")  # F1 returns to the selected second movie.
        time.sleep(.1)
        self.key(b"b")
        self.wait_request("/Items/movie-tricky-1")

    def test_slow_continue_watching_does_not_block_libraries(self):
        self.assertFalse(self.home_gate.is_set())
        self.key(b"\x1b[Cb")  # Browse Movies while the first Continue card loads.
        self.wait_request("/Items", ParentId="view-movies", StartIndex=0)
        self.home_gate.set()
        self.key(b"b")
        self.wait_request("/Items/movie-tricky-0")

    def test_continue_card_is_selected_before_feed_arrives(self):
        self.assertFalse(self.home_gate.is_set())
        self.key(b"b")  # The initial selection must already be Continue.
        time.sleep(.5)
        self.assertFalse(any(urlparse(request).path == "/Items" and
                             parse_qs(urlparse(request).query).get("ParentId") == ["view-movies"]
                             for request in self.requests), "startup opened Movies instead of Continue")
        self.home_gate.set()
        self.wait_request("/Shows/NextUp", enableResumable="false")
        time.sleep(.2)
        self.key(b"b")
        self.wait_request("/Items/movie-tricky-0")
        self.assertFalse(any("misterfin-crt%3Acontinue" in request for request in self.requests))

    def test_combined_continue_watching(self):
        self.wait_request("/UserItems/Resume", MediaTypes="Video")
        self.wait_request("/Shows/NextUp", enableResumable="false")
        # Continue is the initial selection, independent of response order.
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
        self.assertFalse(any("misterfin-crt%3Acontinue" in request for request in self.requests))
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

    def test_transcode_profile_from_configuration(self):
        self.key(b"b")
        self.wait_request("/Items", ParentId="view-movies", StartIndex=0)
        self.key(b"b")
        self.wait_request("/Items/movie-tricky-0")
        self.key(b"b")
        self.wait_request("/Videos/movie-tricky-0/stream", maxWidth=640, maxHeight=480,
                          videoBitRate=8000000, maxFramerate=30, allowVideoStreamCopy="false")
        self.assertEqual(len(self.read_frame()), 640 * 240 * 4)

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

    def test_stop_keeps_browser_responsive_during_slow_save(self):
        self.key(b"b")
        self.wait_request("/Items", ParentId="view-movies", StartIndex=0)
        self.key(b"b")
        self.wait_request("/Items/movie-tricky-0")
        self.key(b"b")
        self.wait_request("/Videos/movie-tricky-0/stream")
        deadline = time.monotonic() + 5
        while not any(path == "/Sessions/Playing" for path, _ in self.reports):
            self.assertLess(time.monotonic(), deadline, "playback did not start")
            time.sleep(0.01)
        self.stop_report_gate.clear()
        self.addCleanup(self.stop_report_gate.set)
        before = sum(urlparse(path).path == "/Items/movie-tricky-0" for path in self.requests)
        started = time.monotonic()
        self.key(b"a")
        while sum(urlparse(path).path == "/Items/movie-tricky-0" for path in self.requests) == before:
            self.assertLess(time.monotonic() - started, 1, "Stop waited for the blocked server report")
            time.sleep(0.01)
        self.assertFalse(any(path.endswith("/UserData") for path, _ in self.reports))
        # Navigate away while stop/save is still blocked on the server.
        self.key(b"aa")
        time.sleep(0.1)
        self.key(b"\x1b[Cb")
        self.wait_request("/Items", ParentId="view-tv", StartIndex=0)
        self.assertLess(time.monotonic() - started, 1, "server cleanup blocked navigation")
        self.process.terminate()
        with self.assertRaises(subprocess.TimeoutExpired):
            self.process.wait(timeout=0.15)
        self.stop_report_gate.set()
        self.process.wait(timeout=2)
        saves = [body for path, body in self.reports if path.endswith("/UserData")]
        self.assertEqual(saves[-1]["PlaybackPositionTicks"], 20000000)

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

    def test_view_back_returns_to_clean_video(self):
        self.key(b"b")
        self.wait_request("/Items", ParentId="view-movies", StartIndex=0)
        self.key(b"b")
        self.wait_request("/Items/movie-tricky-0")
        self.key(b"b")
        self.wait_request("/Videos/movie-tricky-0/stream", startTimeTicks=0)
        clean = bytes([23]) * 640 * 240 * 4
        def wait_frame(predicate, message):
            deadline = time.monotonic() + 2
            while not predicate(self.read_frame()):
                self.assertLess(time.monotonic(), deadline, message)
                time.sleep(.02)

        wait_frame(lambda frame: frame == clean, "video did not start")
        for tab in range(3):
            self.key(b"\x1b[A")  # Show playback controls before entering View.
            self.key(b"\t" + b"\x1b[C" * (tab > 0))
            wait_frame(lambda frame: frame[(40 * 640 + 13) * 4] < 10, "View did not open")
            self.key(b"\x1b[B\x1b[Aa")
            wait_frame(lambda frame: frame == clean, "Back revealed playback controls")
        self.assertIsNone(self.process.poll())

    def test_video_picture_selection(self):
        self.key(b"b")
        self.wait_request("/Items", ParentId="view-movies", StartIndex=0)
        self.key(b"b")
        self.wait_request("/Items/movie-tricky-0")
        self.key(b"b")
        self.wait_request("/Videos/movie-tricky-0/stream", startTimeTicks=0)

        def wait_modes(expected):
            log = self.directory / "picture-modes"
            deadline = time.monotonic() + 5
            while not log.exists() or log.read_text().splitlines() != expected:
                self.assertLess(time.monotonic(), deadline, "decoder did not receive expected picture modes")
                time.sleep(.02)
            time.sleep(.15)  # Deliver the decoder's first position to the controller.

        wait_modes(["False"])
        self.key(b"\t\x1b[C\x1b[C\x1b[Bb")
        wait_modes(["False", "True"])
        streams = lambda: [r for r in self.requests if urlparse(r).path == "/Videos/movie-tricky-0/stream"]
        self.assertEqual(len(streams()), 1, "live Zoom reopened the stream")
        self.assertTrue(self.read_frame() == bytes([23]) * 640 * 240 * 4,
                        "applying Zoom did not return to clean video")
        self.key(b"l")
        self.wait_request("/Videos/movie-tricky-0/stream", startTimeTicks=320000000)
        wait_modes(["False", "True", "True"])
        self.key(b"\t\x1b[Ab")
        wait_modes(["False", "True", "True", "False"])
        self.assertEqual(len(streams()), 2, "live Original reopened the stream")
        self.assertTrue(self.read_frame() == bytes([23]) * 640 * 240 * 4,
                        "applying Original did not return to clean video")
        self.key(b"a")

    def test_video_track_selection(self):
        self.key(b"b")
        self.wait_request("/Items", ParentId="view-movies", StartIndex=0)
        self.key(b"b")
        self.wait_request("/Items/movie-tricky-0")
        self.key(b"b")
        self.wait_request("/Videos/movie-tricky-0/stream", startTimeTicks=0)
        self.key(b"\t\x1b[Bb")
        self.wait_request("/Videos/movie-tricky-0/movie-tricky-0/Subtitles/4/Stream.srt")
        deadline = time.monotonic() + 3
        while not any(body.get("SubtitleStreamIndex") == 4 for _, body in self.reports):
            self.assertLess(time.monotonic(), deadline, "text selection did not finish")
            time.sleep(.02)
        streams = lambda: [parse_qs(urlparse(r).query) for r in self.requests
                           if urlparse(r).path == "/Videos/movie-tricky-0/stream"]
        self.assertEqual(len(streams()), 1, "text subtitle restarted decoding")
        self.key(b"\t\x1b[C\x1b[B\x1b[Bb")
        self.wait_request("/Videos/movie-tricky-0/stream", audioStreamIndex=2, startTimeTicks=20000000)
        self.key(b"l")
        self.wait_request("/Videos/movie-tricky-0/stream", audioStreamIndex=2, startTimeTicks=340000000)
        self.assertEqual(sum("/Subtitles/4/" in r for r in self.requests), 1, "seek reloaded cached text")
        self.reports.clear()
        self.key(b"\t\x1b[D\x1b[Ab")
        deadline = time.monotonic() + 3
        while not any(path.endswith("/Progress") and body.get("SubtitleStreamIndex") == -1
                      for path, body in self.reports):
            self.assertLess(time.monotonic(), deadline, "Off did not finish")
            time.sleep(.02)
        time.sleep(.15)  # Let the subtitle completion event close the picker.
        self.assertEqual(len(streams()), 3, "Off restarted client-rendered text")
        self.key(b"\t" + b"\x1b[B" * 4 + b"b")
        self.wait_request("/Videos/movie-tricky-0/stream", audioStreamIndex=2,
                          subtitleStreamIndex=7, subtitleMethod="Encode", startTimeTicks=360000000)
        self.key(b"a")

    def test_video_choices_survive_stop_and_app_restart(self):
        def wait_for(predicate, message):
            deadline = time.monotonic() + 5
            while not predicate():
                self.assertIsNone(self.process.poll())
                self.assertLess(time.monotonic(), deadline, message)
                time.sleep(.02)

        def open_movie():
            self.key(b"b")
            self.wait_request("/Items", ParentId="view-movies", StartIndex=0)
            self.key(b"b")
            self.wait_request("/Items/movie-tricky-0")
            self.key(b"b")

        def stop_video():
            self.reports.clear()
            self.key(b"a")
            wait_for(lambda: any(path.endswith("/Stopped") for path, _ in self.reports),
                     "playback did not stop")
            time.sleep(.15)

        def verify_restored():
            self.wait_request("/Videos/movie-tricky-0/stream", audioStreamIndex=2)
            self.wait_request("/Videos/movie-tricky-0/movie-tricky-0/Subtitles/4/Stream.srt")
            wait_for(lambda: any(body.get("SubtitleStreamIndex") == 4 for _, body in self.reports),
                     "saved text subtitles were not restored")
            log = self.directory / "picture-modes"
            wait_for(lambda: log.read_text().splitlines()[-1] == "True",
                     "saved Zoom mode did not reach the decoder")

        open_movie()
        self.wait_request("/Videos/movie-tricky-0/stream", startTimeTicks=0)
        self.key(b"\t\x1b[Bb")  # Text subtitle.
        self.wait_request("/Videos/movie-tricky-0/movie-tricky-0/Subtitles/4/Stream.srt")
        self.key(b"\t\x1b[C\x1b[B\x1b[Bb")  # Alternate audio.
        self.wait_request("/Videos/movie-tricky-0/stream", audioStreamIndex=2)
        self.key(b"\t\x1b[C\x1b[Bb")  # Zoom.
        wait_for(lambda: (self.directory / "picture-modes").read_text().splitlines() == ["False", "False", "True"],
                 "Zoom did not start")
        time.sleep(.15)
        stop_video()
        self.requests.clear()
        self.reports.clear()
        (self.directory / "picture-modes").write_text("")
        self.key(b"b")  # Immediate resume.
        verify_restored()
        stop_video()

        self.stop()
        self.assertEqual(self.process.returncode, 0, "shutdown did not flush choices")
        self.requests.clear()
        self.reports.clear()
        (self.directory / "picture-modes").write_text("")
        master, slave = pty.openpty()
        self.master = master
        self.addCleanup(os.close, master)

        def terminal_session():
            os.setsid()
            fcntl.ioctl(0, termios.TIOCSCTTY, 0)

        self.process = subprocess.Popen(
            self.process.args, stdin=slave, stdout=self.log, stderr=self.log,
            preexec_fn=terminal_session,
            # Keep release checks offline. Jellyfin fixtures use loopback HTTP.
            env={**os.environ, "MISTERFIN_CACHE_ROOT": str(self.directory / "cache"), "MISTERFIN_SETTINGS": "",
                 "HTTPS_PROXY": "http://127.0.0.1:1", "NO_PROXY": "127.0.0.1,localhost"},
        )
        os.close(slave)
        self.wait_request("/UserViews")
        open_movie()
        verify_restored()
        stop_video()

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

    def test_mixed_library_movie_series_and_folder_navigation(self):
        self.key(b"b")
        query = self.wait_request("/Items", ParentId="view-mixed", StartIndex=0, Limit=64)
        self.assertNotIn("IncludeItemTypes", query)
        self.assertNotIn("Recursive", query)
        self.key(b"b")
        self.wait_request("/Items/movie-tricky-0")
        self.key(b"a\x1b[Bb")
        self.wait_request("/Shows/series-000/Seasons")
        self.key(b"b")
        self.wait_request("/Shows/series-000/Episodes", seasonId="series-000-s1")
        self.key(b"aa\x1b[Bb")
        query = self.wait_request("/Items", ParentId="mixed-folder", StartIndex=0, Limit=64)
        self.assertNotIn("IncludeItemTypes", query)
        self.assertNotIn("Recursive", query)
        self.key(b"b")
        self.wait_request("/Items/movie-tricky-1")
        self.assertFalse(any(urlparse(r).path == "/Items/artist-001" for r in self.requests))

    def test_live_tv_uses_channels_endpoint(self):
        self.key(b"\x1b[C\x1b[C\x1b[Cb")
        params = self.wait_request("/LiveTv/Channels", StartIndex=0, Limit=64)
        self.assertEqual(params["AddCurrentProgram"], ["true"])
        self.assertNotIn("SortBy", params)
        self.assertFalse(any(parse_qs(urlparse(r).query).get("ParentId") == ["view-live-tv"]
                             for r in self.requests))

    def test_live_captions_toggle_without_retuning(self):
        self.key(b"\x1b[C\x1b[C\x1b[Cb")
        self.wait_request("/LiveTv/Channels", StartIndex=0)
        self.key(b"b")
        self.wait_request("/Videos/channel-2-1/stream.ts")
        clean = bytes([23]) * 640 * 240 * 4
        deadline = time.monotonic() + 3
        while self.read_frame() != clean:
            self.assertLess(time.monotonic(), deadline, "clean video did not appear")
            time.sleep(.02)
        self.key(b"\t\x1b[Bb")  # Enable live captions.
        deadline = time.monotonic() + 3
        while True:
            frame = self.read_frame()
            if frame[:120*640*4] == clean[:120*640*4] and frame != clean:
                break
            self.assertLess(time.monotonic(), deadline, "caption overlay did not appear")
            time.sleep(.02)
        self.key(b"\t\x1b[Ab")  # Off clears the shared overlay.
        deadline = time.monotonic() + 3
        while self.read_frame() != clean:
            self.assertLess(time.monotonic(), deadline, "caption overlay did not clear")
            time.sleep(.02)
        self.assertEqual(sum(urlparse(r).path == "/Videos/channel-2-1/stream.ts" for r in self.requests), 1)
        self.assertFalse(any(path == "/LiveStreams/Close" for path, _ in self.reports))
        self.key(b"a")

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

    def test_remote_playback_queue(self):
        def wait_for(predicate, message):
            deadline = time.monotonic() + 5
            while not predicate():
                self.assertLess(time.monotonic(), deadline, message)
                time.sleep(.02)

        def send(kind, data):
            self.remote_commands.put({"MessageType": kind, "Data": data})

        def latest_progress():
            return next((body for path, body in reversed(self.reports)
                         if path == "/Sessions/Playing/Progress"), {})

        wait_for(lambda: any(path == "/Sessions/Capabilities/Full" for path, _ in self.reports), "remote capability registration missing")
        tracks = ["artist-000-album0-t01", "artist-000-album0-t02"]
        send("Play", {"PlayCommand": "PlayNow", "ItemIds": tracks, "StartIndex": 1})
        wait_for(lambda: latest_progress().get("ItemId") == tracks[1], "remote playback did not start")
        self.assertEqual([item["Id"] for item in latest_progress()["NowPlayingQueue"]], tracks)
        self.assertTrue(latest_progress()["PlaylistItemId"])
        send("Playstate", {"Command": "PreviousTrack"})
        wait_for(lambda: latest_progress().get("ItemId") == tracks[0], "previous required multiple commands")
        send("Playstate", {"Command": "Pause"})
        send("Playstate", {"Command": "Pause"})
        wait_for(lambda: latest_progress().get("IsPaused"), "remote pause failed")
        send("Playstate", {"Command": "Unpause"})
        wait_for(lambda: latest_progress().get("IsPaused") is False, "remote resume failed")
        send("Play", {"PlayCommand": "PlayNext", "ItemIds": [tracks[0]]})
        wait_for(lambda: len(latest_progress().get("NowPlayingQueue", [])) == 3, "queue next failed")
        send("GeneralCommand", {"Name": "SetRepeatMode", "Arguments": {"RepeatMode": "RepeatAll"}})
        wait_for(lambda: latest_progress().get("RepeatMode") == "RepeatAll", "repeat state not reported")
        send("GeneralCommand", {"Name": "SetShuffleQueue", "Arguments": {"ShuffleMode": "Shuffle"}})
        wait_for(lambda: latest_progress().get("PlaybackOrder") == "Shuffle", "shuffle state not reported")
        send("Playstate", {"Command": "Stop"})
        wait_for(lambda: any(path == "/Sessions/Playing/Stopped" and body.get("ItemId") == tracks[0]
                             for path, body in self.reports), "remote stop failed")
        self.assertIsNone(self.process.poll(), "remote Stop exited the browser")
        self.remote_done.set()

    def test_music_plays_with_browser_frame_and_stops(self):
        self.exercise_music_playback()

    def exercise_music_playback(self):
        """Play, pause, change tracks, and stop using the application's real input loop."""
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
        if self._testMethodName == "test_music_plays_with_browser_frame_and_stops":
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
