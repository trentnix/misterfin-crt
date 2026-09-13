#!/usr/bin/env python3
"""Unit tests for the Ghostty framebuffer presenter."""

from __future__ import annotations

import base64
import io
import importlib.util
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch


MODULE_PATH = Path(__file__).with_name("ghostty_harness.py")
SPEC = importlib.util.spec_from_file_location("ghostty_harness", MODULE_PATH)
assert SPEC and SPEC.loader
HARNESS = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(HARNESS)


class ConversionTests(unittest.TestCase):
    def test_bgrx_to_rgb_reorders_pixels(self):
        frame = bytes((3, 2, 1, 0, 30, 20, 10, 255))
        self.assertEqual(
            HARNESS.bgrx_to_rgb(frame, 2, 1),
            bytes((1, 2, 3, 10, 20, 30)),
        )

    def test_bgrx_to_rgb_rejects_wrong_size(self):
        with self.assertRaisesRegex(ValueError, "expected 8 frame bytes"):
            HARNESS.bgrx_to_rgb(b"short", 2, 1)


class LaunchOptionsTests(unittest.TestCase):
    def test_demo_selects_go_browser(self):
        args = HARNESS.parse_args(["--demo", "--ntsc"])
        self.assertTrue(args.browse)
        self.assertTrue(args.demo)
        self.assertEqual(args.binary, HARNESS.REPO_ROOT / "build/misterfin-crt")

    def test_browse_preserves_explicit_config(self):
        args = HARNESS.parse_args(["--browse", "--config", "/tmp/jellyfin.conf"])
        self.assertEqual(args.config, Path("/tmp/jellyfin.conf"))

    def test_inline_video_rate_can_be_overridden(self):
        args = HARNESS.parse_args(["--browse", "--inline-video"])
        self.assertEqual(args.fps, 60)
        args = HARNESS.parse_args(["--browse", "--inline-video", "--fps", "25"])
        self.assertEqual(args.fps, 25)

    def test_nonfinite_rates_are_rejected(self):
        for value in ("nan", "inf", "0", "-1"):
            with self.subTest(value=value), patch("sys.stderr", io.StringIO()):
                with self.assertRaises(SystemExit):
                    HARNESS.parse_args(["--fps", value])

    def test_go_test_frame_is_default(self):
        args = HARNESS.parse_args([])
        self.assertFalse(args.browse)
        self.assertEqual(args.binary, HARNESS.REPO_ROOT / "build/misterfin-crt")

    def test_legacy_go_flag_still_selects_test_frame(self):
        args = HARNESS.parse_args(["--go", "--ntsc"])
        self.assertTrue(args.ntsc)
        self.assertEqual(args.binary, HARNESS.REPO_ROOT / "build/misterfin-crt")

    def test_explicit_binary_is_preserved(self):
        args = HARNESS.parse_args(["--go", "--binary", "/tmp/custom-go"])
        self.assertEqual(args.binary, Path("/tmp/custom-go"))


class TimingTests(unittest.TestCase):
    def test_upload_cost_does_not_slow_the_presentation_clock(self):
        deadline = 10.0
        for _ in range(60):
            # Eight milliseconds of upload work must fit within each interval,
            # rather than adding another eight milliseconds to every interval.
            deadline = HARNESS.next_frame_deadline(deadline, deadline + 0.008, 1 / 60)
        self.assertAlmostEqual(deadline, 11.0)

    def test_stall_skips_expired_slots_without_a_catchup_burst(self):
        deadline = HARNESS.next_frame_deadline(10.0, 10.075, 1 / 60)
        self.assertAlmostEqual(deadline, 10 + 5 / 60)
        self.assertGreater(deadline, 10.075)
        self.assertLessEqual(deadline - 10.075, 1 / 60)


class ProtocolTests(unittest.TestCase):
    def test_empty_command_has_no_continuation_field(self):
        chunks = list(HARNESS.kitty_chunks("a=d,d=i,i=1"))
        self.assertEqual(chunks, [b"\x1b_Ga=d,d=i,i=1;\x1b\\"])

    def test_payload_is_split_and_reassembles(self):
        payload = bytes(range(256)) * 40
        chunks = list(HARNESS.kitty_chunks("a=T,f=24", payload))

        self.assertGreater(len(chunks), 1)
        self.assertIn(b"a=T,f=24,m=1;", chunks[0])
        self.assertIn(b"m=0;", chunks[-1])

        encoded = b"".join(chunk.split(b";", 1)[1][:-2] for chunk in chunks)
        self.assertEqual(base64.b64decode(encoded), payload)

    def test_second_frame_is_placed_before_the_old_image_is_deleted(self):
        tty = io.BytesIO()
        presenter = HARNESS.GhosttyPresenter(tty, 1, 1)
        original_geometry = HARNESS.terminal_geometry
        HARNESS.terminal_geometry = lambda _tty: (80, 24, 0, 0)
        try:
            presenter.show(bytes((3, 2, 1, 0)))
            first_output = tty.getvalue()
            tty.seek(0)
            tty.truncate()

            presenter.show(bytes((6, 5, 4, 0)))
            second_output = tty.getvalue()
        finally:
            HARNESS.terminal_geometry = original_geometry

        self.assertIn(b"a=T", first_output)
        self.assertNotIn(b"a=d", first_output)
        self.assertIn(b"a=t", second_output)
        self.assertIn(b"a=p", second_output)
        self.assertIn(b"a=d", second_output)
        self.assertLess(second_output.index(b"a=p"), second_output.index(b"a=d"))

        # Uploading is invisible. Both visible mutations must commit inside
        # one synchronized update, with no intermediate placement on screen.
        begin = second_output.index(HARNESS.SYNC_BEGIN)
        end = second_output.index(HARNESS.SYNC_END)
        self.assertLess(second_output.index(b"a=t"), begin)
        self.assertLess(begin, second_output.index(b"a=p"))
        self.assertLess(second_output.index(b"a=d"), end)
        self.assertEqual(second_output.count(HARNESS.SYNC_BEGIN), 1)
        self.assertEqual(second_output.count(HARNESS.SYNC_END), 1)

    def test_placement_enforces_the_four_by_three_cell_rectangle(self):
        tty = io.BytesIO()
        presenter = HARNESS.GhosttyPresenter(tty, 640, 288)
        original_geometry = HARNESS.terminal_geometry
        HARNESS.terminal_geometry = lambda _tty: (80, 24, 0, 0)
        try:
            presenter.show(bytes(640 * 288 * 4))
        finally:
            HARNESS.terminal_geometry = original_geometry

        control = tty.getvalue().split(b";", 1)[0]
        self.assertIn(b",c=64,r=24,", control)


class GeometryTests(unittest.TestCase):
    def test_four_by_three_image_fits_typical_terminal_cells(self):
        self.assertEqual(HARNESS.display_cells(80, 24), (64, 24))

    def test_image_is_limited_by_terminal_height(self):
        self.assertEqual(HARNESS.display_cells(120, 10), (27, 10))

    def test_reported_pixel_geometry_is_used(self):
        self.assertEqual(
            HARNESS.display_cells(100, 40, 1000, 800),
            (100, 38),
        )


class FrameReadTests(unittest.TestCase):
    def test_complete_frame_is_returned(self):
        with tempfile.TemporaryDirectory() as temp_dir:
            path = Path(temp_dir) / "frame.raw"
            path.write_bytes(b"frame")
            self.assertEqual(HARNESS.read_complete_frame(path, 5), b"frame")

    def test_missing_and_short_frames_are_ignored(self):
        with tempfile.TemporaryDirectory() as temp_dir:
            path = Path(temp_dir) / "frame.raw"
            self.assertIsNone(HARNESS.read_complete_frame(path, 5))
            path.write_bytes(b"bad")
            self.assertIsNone(HARNESS.read_complete_frame(path, 5))


class FrameWatchTests(unittest.TestCase):
    def test_notifies_after_a_complete_write(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "frame"
            with HARNESS.FrameWatch(path) as watcher:
                with path.open("wb") as source:
                    source.write(b"frame")
                    source.flush()
                    self.assertFalse(watcher.wait(0))
                self.assertTrue(watcher.wait(0.5))
                self.assertEqual(HARNESS.read_complete_frame(path, 5), b"frame")
            self.assertEqual(watcher.fd, -1)

    def test_follows_replacements_and_ignores_other_files(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "frame"
            temporary = Path(directory) / "next"
            with HARNESS.FrameWatch(path) as watcher:
                for value in (b"first", b"second"):
                    temporary.write_bytes(value)
                    self.assertFalse(watcher.wait(0.1))
                    temporary.replace(path)
                    self.assertTrue(watcher.wait(0.5))
                    self.assertEqual(path.read_bytes(), value)
class ChildEnvironmentTests(unittest.TestCase):
    def test_desktop_cache_root_is_set_by_default(self):
        with patch.dict("os.environ", {}, clear=True):
            env = HARNESS.child_environment(640, 288, Path("/tmp/frame.raw"))
        self.assertEqual(env["MISTERFIN_CACHE_ROOT"], "/tmp/misterfin-cache")

    def test_explicit_cache_root_is_preserved(self):
        with patch.dict("os.environ", {"MISTERFIN_CACHE_ROOT": "/tmp/custom-cache"}, clear=True):
            env = HARNESS.child_environment(640, 240, Path("/tmp/frame.raw"))
        self.assertEqual(env["MISTERFIN_CACHE_ROOT"], "/tmp/custom-cache")


if __name__ == "__main__":
    unittest.main()
