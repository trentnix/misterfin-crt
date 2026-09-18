"""Integration checks for the built Go browser against the inherited mock server."""

import base64
from dataclasses import dataclass, field
import fcntl
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import pty
import queue
import select
import struct
import subprocess
import tempfile
import termios
import threading
import time
import unittest
from http.server import ThreadingHTTPServer
from urllib.parse import urlparse, parse_qs

ROOT = Path(__file__).resolve().parents[3]
BINARY = Path(os.environ.get("MISTERVISION_TEST_BINARY", str(ROOT / "build/mistervision")))


@dataclass
class Scenario:
    """Explicit settings, server behavior, and player choices for one browser run."""

    settings: dict = field(default_factory=dict)
    files: dict = field(default_factory=dict)
    background_image: bool = False
    resume_movie: bool = False
    short_album: bool = False
    continue_items: bool = False
    mixed_library: bool = False
    hold_home: bool = False
    remote_control: bool = False
    slow_seek: bool = False
    page_delay: float = 0
    video_delay: float = 0
    transcode_profile: str = ""
    media: bytes | None = None
    player: str = "idle"
    controlling_terminal: bool = True
    legacy_settings: bool = False
    unified_server: bool = False


class BrowserFixture(unittest.TestCase):
    """Own a mock server, temporary files, and a browser with applied-event waits.

    Tests call start_browser explicitly. Cleanup also runs after failed assertions.
    Players are synthetic by default. The decode scenario runs libmpv with null
    audio. No scenario opens the workstation audio device.
    """

    def start_browser(self, scenario=None):
        """Start one isolated scenario. Defaults use current settings and silent cues."""
        self.scenario = scenario or Scenario()
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.directory = Path(self.temp.name)
        spec = importlib.util.spec_from_file_location("mock_jellyfin", ROOT / "tools/mock-jellyfin.py")
        mock = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(mock)
        mock.ITEMS.update({channel["Id"]: channel for channel in mock.LIVE_CHANNELS})
        if self.scenario.background_image:
            (self.directory / "background.png").write_bytes(mock._png(200, 40, 20, 8, 6))
        for name, contents in self.scenario.files.items():
            (self.directory / name).write_bytes(contents)
        if self.scenario.resume_movie:
            mock.ITEMS["movie-tricky-0"]["UserData"]["PlaybackPositionTicks"] = 600000000
        if self.scenario.short_album:
            mock.CHILDREN["artist-000-album0"] = mock.CHILDREN["artist-000-album0"][:2]
        if self.scenario.continue_items:
            for item_id in ("movie-tricky-0", "series-000-s1e01"):
                mock.ITEMS[item_id]["UserData"]["PlaybackPositionTicks"] = 600000000
                mock.ITEMS[item_id]["UserData"]["LastPlayedDate"] = "2026-09-12T12:00:00Z"
        if self.scenario.mixed_library:
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
        if not self.scenario.hold_home:
            self.home_gate.set()
        self.addCleanup(self.home_gate.set)
        self.video_response_gate = None
        self.media_unavailable = threading.Event()
        self.stop_report_gate = threading.Event()
        self.stop_report_gate.set()
        test = self

        class Handler(mock.Handler):
            def handle(self):
                try:
                    super().handle()
                except (BrokenPipeError, ConnectionResetError):
                    pass  # Cancellation can also close the next keep-alive read.

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
                if path == "/socket" and test.scenario.remote_control:
                    # Support client heartbeats during endurance runs. Full protocol,
                    # TLS, and reconnection behavior remain covered by Go tests.
                    key = self.headers["Sec-WebSocket-Key"]
                    accept = base64.b64encode(hashlib.sha1((key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11").encode()).digest()).decode()
                    self.send_response(101)
                    self.send_header("Upgrade", "websocket")
                    self.send_header("Connection", "Upgrade")
                    self.send_header("Sec-WebSocket-Accept", accept)
                    self.end_headers()
                    self.close_connection = True
                    incoming = bytearray()
                    while not test.remote_done.is_set():
                        if select.select([self.connection], [], [], 0)[0]:
                            packet = self.connection.recv(4096)
                            if not packet:
                                return
                            incoming.extend(packet)
                            for opcode, payload in websocket_frames(incoming):
                                if opcode == 8:
                                    return
                                if opcode == 9:
                                    self.wfile.write(bytes([0x8a, len(payload)]) + payload)
                                    self.wfile.flush()
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
                if (test.scenario.mixed_library
                        and path == "/Items" and query.get("ParentId") == ["view-mixed"]
                        and "MusicArtist" in query.get("IncludeItemTypes", [""])[0].split(",")):
                    # Reproduce the unrelated artist rows returned by the real server.
                    return self._send(self._query_result(["artist-001"], query))
                if path == "/Items" and query.get("SortBy") == ["Random"]:
                    ids = ["artist-000-album0-t01", "artist-001-album0-t01"]
                    return self._send(self._query_result(ids, query))
                if path in ("/UserItems/Resume", "/Shows/NextUp"):
                    test.home_gate.wait(timeout=5)
                    if not test.scenario.continue_items:
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
                    if test.media_unavailable.is_set():
                        return self._send({"error": "fixture unavailable"}, status=503)
                    if test.scenario.video_delay:
                        time.sleep(test.scenario.video_delay)
                    if (test.scenario.slow_seek and
                            parse_qs(urlparse(self.path).query).get("startTimeTicks") == ["920000000"]):
                        time.sleep(16)  # Exceed the former media response-header timeout.
                    gate = test.video_response_gate
                    if gate is not None:
                        gate.wait(timeout=3)
                    try:
                        self.send_response(200)
                        payload = test.scenario.media if test.scenario.media is not None else b"test video"
                        self.send_header("Content-Length", str(len(payload)))
                        self.end_headers()
                        self.wfile.write(payload)
                    except (BrokenPipeError, ConnectionResetError):
                        pass
                    return
                if (test.scenario.page_delay
                        and (path == "/UserViews" or path == "/LiveTv/Channels" or
                             (path.startswith("/Shows/") and path.endswith(("/Seasons", "/Episodes"))) or
                             (path == "/Items" and query.get("StartIndex") == ["0"]
                              and query.get("Limit") == ["64"]))):
                    time.sleep(test.scenario.page_delay)
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
        config = self.directory / "jellyfin.conf"
        config.write_text(f"http://127.0.0.1:{self.server.server_port}\nmock-api-key\nmockuser\n")
        if self.scenario.transcode_profile:
            with config.open("a") as config_file:
                config_file.write(self.scenario.transcode_profile + "\n")
        self.frame = self.directory / "frame.raw"
        player_args = self.install_player()
        master, slave = pty.openpty()
        self.master = master
        self.addCleanup(os.close, master)
        self.log = (self.directory / "browser.log").open("w+b")
        self.addCleanup(self.log.close)

        controlling_terminal = self.scenario.controlling_terminal

        def terminal_session():
            os.setsid()
            if controlling_terminal:
                fcntl.ioctl(0, termios.TIOCSCTTY, 0)

        # Browser milestones synchronize input with applied results, rather
        # than assuming a loopback response is rendered within a fixed delay.
        self.diagnostics = self.directory / "logs" / "diagnostics.log"
        self.write_settings()
        if self.scenario.unified_server:
            config.unlink()  # The shared document must be sufficient on its own.
        self.environment = {**os.environ, "MISTERVISION_CACHE_ROOT": str(self.directory / "cache"),
                            "MISTERVISION_SETTINGS": "", "MISTERVISION_INPUT_CONFIG": "",
                            "MISTERVISION_SOUND_CONFIG": "", "MISTERVISION_MUSIC_CONFIG": "",
                            "HTTPS_PROXY": "http://127.0.0.1:1", "NO_PROXY": "127.0.0.1,localhost"}

        self.process = subprocess.Popen(
            [str(BINARY), "-browse", "-headless", "640x240", "-output", str(self.frame),
             "-config", str(config), "-state-dir", str(self.directory / "state")] + player_args,
            stdin=slave, stdout=self.log, stderr=self.log, preexec_fn=terminal_session,
            # Keep release checks offline. Jellyfin fixtures use loopback HTTP.
            env=self.environment,
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
                    elif path == "/LiveTv/Channels" and params.get("Limit") == ["64"]:
                        self.wait_event("browser.page", kind="livetv",
                                        start=int(params.get("StartIndex", ["0"])[0]), failed=False)
                    elif path.startswith("/Shows/") and path.endswith("/Seasons"):
                        self.wait_event("browser.page", kind="seasons", parent=path.split("/")[2],
                                        start=0, failed=False)
                    elif path.startswith("/Shows/") and path.endswith("/Episodes"):
                        self.wait_event("browser.page", kind="episodes", parent=params["seasonId"][0],
                                        start=int(params.get("StartIndex", ["0"])[0]), failed=False)
                    else:
                        # Requests alone do not prove playback or rendering is
                        # ready. Tests that send dependent input must also wait
                        # for an applied event or the expected frame.
                        time.sleep(0.15)
                    self.assertIsNone(self.process.poll())
                    return params
            if self.process.poll() is not None:
                self.log.seek(0)
                self.fail(self.log.read().decode())
            time.sleep(0.01)
        self.fail(f"request not observed: {path} {query}; got {self.requests}")

    def wait_video_ready(self, start_ticks=0):
        """Wait for the inline fixture's two-second position to reach the UI loop."""
        self.wait_event("browser.playback-ready", position_ticks=start_ticks + 20000000)

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

    def install_player(self):
        """Copy a readable fixture program to the isolated executable/helper path."""
        if self.scenario.player == "decode":
            # Use production libmpv decoding but never open workstation audio.
            player = self.directory / "test-player.py"
            helper = ROOT / "tools/ghostty/video_player.py"
            player.write_text("import runpy, sys\nsys.argv += ['--audio', 'null']\n"
                              + f"runpy.run_path({str(helper)!r}, run_name='__main__')\n")
            return ["-terminal-player", str(player)]
        programs = {"idle": "idle_player.sh", "finish": "finish_player.sh",
                    "inline": "inline_player.py", "buffering": "buffering_player.py"}
        program = Path(__file__).parent / programs[self.scenario.player]
        player = self.directory / "test-player"
        player.write_bytes(program.read_bytes())
        player.chmod(0o700)
        option = "-terminal-player" if program.suffix == ".py" else "-player"
        return [option, str(player)]

    def write_settings(self):
        """Write current settings directly. The legacy scenario uses old files only."""
        diagnostics = {"enabled": True, "path": "logs/diagnostics.log", "max_bytes": 65536}
        backgrounds = [{"name": "Off", "type": "none"}]
        if self.scenario.legacy_settings:
            if self.scenario.settings:
                raise ValueError("legacy fixtures must specify their own legacy values")
            legacy = {"sounds": {"enabled": False},
                      "music": {"default": "Off", "meters": False, "backgrounds": backgrounds},
                      "diagnostics": diagnostics}
            for name, value in legacy.items():
                (self.directory / f"{name}.json").write_text(json.dumps(value))
            return
        settings = {"ui": {"navigation_sounds": {"enabled": False}},
                    "music_visuals": {"default_background": "Off", "show_audio_meters": False,
                                      "backgrounds": backgrounds},
                    "diagnostics": diagnostics}
        if self.scenario.unified_server:
            settings["server"] = {
                "provider": "jellyfin",
                "url": f"http://127.0.0.1:{self.server.server_port}",
                "jellyfin": {"api_key": "mock-api-key", "username": "mockuser"},
                "transcode": {"max_width": 640, "max_height": 480, "video_bitrate": 8000000},
            }
        for section, value in self.scenario.settings.items():
            # A custom title must not accidentally enable workstation sound.
            if section == "ui" and isinstance(value, dict):
                settings["ui"].update(value)
            else:
                settings[section] = value
        (self.directory / "settings.json").write_text(json.dumps(settings))


def websocket_frames(buffer):
    """Consume complete masked client frames for the loopback fixture's heartbeats."""
    while len(buffer) >= 2:
        opcode, size = buffer[0] & 15, buffer[1] & 127
        if not buffer[1] & 128:
            raise ValueError("fixture expects masked client frames")
        header = 2
        if size in (126, 127):
            count = 2 if size == 126 else 8
            if len(buffer) < header + count:
                return
            size = int.from_bytes(buffer[header:header + count], 'big')
            header += count
        if size > 1 << 20 or (opcode >= 8 and size > 125):
            raise ValueError("fixture frame exceeds limit")
        if len(buffer) < header + 4 + size:
            return
        mask = buffer[header:header + 4]
        payload = bytes(b ^ mask[i % 4] for i, b in enumerate(buffer[header + 4:header + 4 + size]))
        del buffer[:header + 4 + size]
        yield opcode, payload
