"""Browser integration scenarios against an isolated Jellyfin fixture."""

import fcntl
import json
import os
import pty
import subprocess
import termios
import threading
import time
import unittest
from urllib.parse import urlparse, parse_qs

if __package__:
    from .fixtures.browser import BINARY, BrowserFixture, Scenario
else:
    from fixtures.browser import BINARY, BrowserFixture, Scenario


@unittest.skipUnless(BINARY.is_file(), "build the Go host binary first")
class BrowseIntegrationTests(BrowserFixture):
    def test_custom_browsing_background(self):
        self.start_browser(Scenario(
            background_image=True,
            settings={"background": {"image": "background.png"}},
        ))
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
        self.start_browser(Scenario(settings={"background": {"image": "missing.png"}}))
        self.wait_event("configuration.fallback", configuration="background",
                        error_kind="not-found", fallback="normal-artwork")
        self.assert_normal_artwork_and_navigation()

    def test_non_image_background_falls_back(self):
        self.start_browser(Scenario(
            settings={"background": {"image": "not-an-image.txt"}},
            files={"not-an-image.txt": b"private non-image contents"},
        ))
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
        self.start_browser(Scenario(settings={
            "ui": {"title": 42, "navigation_sounds": {"volume": 999}},
            "music_visuals": {"backgrounds": []},
        }))
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
        self.exercise_music_playback(clean_footer=False)

    def test_missing_music_asset_keeps_playback(self):
        self.start_browser(Scenario(settings={"music_visuals": {
            "default_background": "Custom",
            "show_audio_meters": False,
            "backgrounds": [
                {"name": "Custom", "type": "image", "files": ["missing.png"]},
                {"name": "Off", "type": "none"},
            ],
        }}))
        self.wait_event("configuration.fallback", configuration="music_visuals",
                        error_kind="not-found", fallback="selected-background-unavailable")
        self.exercise_music_playback(clean_footer=False)

    def test_legacy_settings_still_load(self):
        self.start_browser(Scenario(legacy_settings=True))
        self.assertFalse((self.directory / "settings.json").exists())
        self.key(b"b")
        self.wait_request("/Items", ParentId="view-movies", StartIndex=0)
        self.assertTrue(self.diagnostics.exists())

    def test_diagnostics_lifecycle(self):
        self.start_browser()
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
        self.start_browser(Scenario(page_delay=.3))
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
        self.start_browser(Scenario(hold_home=True, page_delay=.3))
        self.assertFalse(self.home_gate.is_set())
        self.key(b"\x1b[Cb")  # Browse Movies while the first Continue card loads.
        self.wait_request("/Items", ParentId="view-movies", StartIndex=0)
        self.home_gate.set()
        self.key(b"b")
        self.wait_request("/Items/movie-tricky-0")

    def test_continue_card_is_selected_before_feed_arrives(self):
        self.start_browser(Scenario(hold_home=True, continue_items=True))
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
        self.assertFalse(any("mistervision%3Acontinue" in request for request in self.requests))

    def test_combined_continue_watching(self):
        self.start_browser(Scenario(continue_items=True))
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
        self.assertFalse(any("mistervision%3Acontinue" in request for request in self.requests))
        self.assertFalse(any(urlparse(request).path == "/Items/series-000-s1e02" for request in self.requests))

    def test_terminal_stdin_without_controlling_terminal(self):
        self.start_browser(Scenario(controlling_terminal=False))
        # Scripts launch can pass terminal stdin without a controlling terminal.
        self.key(b"b")
        self.wait_request("/Items", ParentId="view-movies", StartIndex=0)
        self.assertEqual(len(self.read_frame()), 640 * 240 * 4)
        self.key(b"q")
        self.assertEqual(self.process.wait(timeout=3), 0)

    def test_movie_paging_and_music_hierarchy(self):
        self.start_browser(Scenario(page_delay=.3))
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
        self.start_browser()
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

    def test_unified_connection_without_legacy_file(self):
        self.start_browser(Scenario(unified_server=True))
        self.assertFalse((self.directory / "jellyfin.conf").exists())
        self.key(b"b")
        self.wait_request("/Items", ParentId="view-movies", StartIndex=0)
        self.key(b"b")
        self.wait_request("/Items/movie-tricky-0")
        self.key(b"b")
        self.wait_request("/Videos/movie-tricky-0/stream", maxWidth=640, maxHeight=480, videoBitRate=8000000)
        self.key(b"a")
        self.wait_request("/Items/movie-tricky-0")
        self.assertIsNone(self.process.poll())

    def test_transcode_profile_from_configuration(self):
        self.start_browser(Scenario(transcode_profile="640x480@8000000"))
        self.key(b"b")
        self.wait_request("/Items", ParentId="view-movies", StartIndex=0)
        self.key(b"b")
        self.wait_request("/Items/movie-tricky-0")
        self.key(b"b")
        self.wait_request("/Videos/movie-tricky-0/stream", maxWidth=640, maxHeight=480,
                          videoBitRate=8000000, maxFramerate=30, allowVideoStreamCopy="false")
        self.assertEqual(len(self.read_frame()), 640 * 240 * 4)

    def test_playback_stop_returns_to_details(self):
        self.start_browser()
        self.exercise_playback_stop()

    def exercise_playback_stop(self):
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
        if self.scenario.player == "inline":
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
        if self.scenario.player == "inline":
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
        self.start_browser()
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
        self.start_browser(Scenario(resume_movie=True, slow_seek=True, page_delay=.3))
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
        self.start_browser()
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
        self.start_browser()
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
        self.start_browser(Scenario(player="inline"))
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
        self.start_browser(Scenario(player="inline"))
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
        self.start_browser(Scenario(player="inline", video_delay=.3))
        self.key(b"b")
        self.wait_request("/Items", ParentId="view-movies", StartIndex=0)
        self.key(b"b")
        self.wait_request("/Items/movie-tricky-0")
        self.key(b"b")
        self.wait_request("/Videos/movie-tricky-0/stream", startTimeTicks=0)
        self.wait_video_ready()
        self.key(b"\t\x1b[Bb")
        self.wait_request("/Videos/movie-tricky-0/movie-tricky-0/Subtitles/4/Stream.srt")
        deadline = time.monotonic() + 3
        while not any(body.get("SubtitleStreamIndex") == 4 for _, body in self.reports):
            self.assertLess(time.monotonic(), deadline, "text selection did not finish")
            time.sleep(.02)
        self.wait_event("browser.subtitle", index=4)
        streams = lambda: [parse_qs(urlparse(r).query) for r in self.requests
                           if urlparse(r).path == "/Videos/movie-tricky-0/stream"]
        self.assertEqual(len(streams()), 1, "text subtitle restarted decoding")
        self.key(b"\t\x1b[C\x1b[B\x1b[Bb")
        self.wait_request("/Videos/movie-tricky-0/stream", audioStreamIndex=2, startTimeTicks=20000000)
        self.wait_video_ready(20000000)
        self.key(b"l")
        self.wait_request("/Videos/movie-tricky-0/stream", audioStreamIndex=2, startTimeTicks=340000000)
        self.wait_video_ready(340000000)
        self.assertEqual(sum("/Subtitles/4/" in r for r in self.requests), 1, "seek reloaded cached text")
        self.reports.clear()
        self.key(b"\t\x1b[D\x1b[Ab")
        deadline = time.monotonic() + 3
        while not any(path.endswith("/Progress") and body.get("SubtitleStreamIndex") == -1
                      for path, body in self.reports):
            self.assertLess(time.monotonic(), deadline, "Off did not finish")
            time.sleep(.02)
        self.wait_event("browser.subtitle", index=-1)
        self.assertEqual(len(streams()), 3, "Off restarted client-rendered text")
        self.key(b"\t" + b"\x1b[B" * 4 + b"b")
        self.wait_request("/Videos/movie-tricky-0/stream", audioStreamIndex=2,
                          subtitleStreamIndex=7, subtitleMethod="Encode", startTimeTicks=360000000)
        self.key(b"a")

    def test_video_choices_survive_stop_and_app_restart(self):
        self.start_browser(Scenario(player="inline"))
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
            env=self.environment,
        )
        os.close(slave)
        self.wait_request("/UserViews")
        self.wait_event("browser.home", failed=False)
        open_movie()
        verify_restored()
        stop_video()

    def test_video_loading_and_buffering_animation(self):
        self.start_browser(Scenario(player="buffering"))
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
        self.start_browser(Scenario(player="inline"))
        self.exercise_playback_stop()

    def test_mixed_library_movie_series_and_folder_navigation(self):
        self.start_browser(Scenario(mixed_library=True, page_delay=.3))
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
        self.start_browser()
        self.key(b"\x1b[C\x1b[C\x1b[Cb")
        params = self.wait_request("/LiveTv/Channels", StartIndex=0, Limit=64)
        self.assertEqual(params["AddCurrentProgram"], ["true"])
        self.assertNotIn("SortBy", params)
        self.assertFalse(any(parse_qs(urlparse(r).query).get("ParentId") == ["view-live-tv"]
                             for r in self.requests))

    def test_live_captions_toggle_without_retuning(self):
        self.start_browser(Scenario(player="inline"))
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
        self.start_browser(Scenario(page_delay=.3))
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
        self.start_browser()
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
        self.start_browser(Scenario(remote_control=True))
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
        self.start_browser()
        self.exercise_music_playback()

    def exercise_music_playback(self, clean_footer=True):
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
        if clean_footer:
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
        self.start_browser()
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
        self.start_browser(Scenario(short_album=True, player="finish"))
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
        self.start_browser()
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
