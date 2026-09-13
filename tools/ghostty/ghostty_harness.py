#!/usr/bin/env python3
"""Run MiSTerFin's desktop harness as an interactive image in Ghostty."""

from __future__ import annotations

import argparse
import base64
import ctypes as C
from contextlib import ExitStack
import fcntl
import importlib.util
import math
import os
from pathlib import Path
import shutil
import select
import signal
import struct
import subprocess
import sys
import tempfile
import termios
import time
import threading
from http.server import ThreadingHTTPServer
from typing import BinaryIO, Iterable


ESC = b"\x1b"
ST = ESC + b"\\"
SYNC_BEGIN = ESC + b"[?2026h"
SYNC_END = ESC + b"[?2026l"
IMAGE_IDS = (0x4D465001, 0x4D465002)
PLACEMENT_ID = 1
DEFAULT_FPS = 20.0
VIDEO_FPS = 60.0
DISPLAY_ASPECT = 4.0 / 3.0
REPO_ROOT = Path(__file__).resolve().parents[2]


def bgrx_to_rgb(frame: bytes, width: int, height: int) -> bytes:
    """Convert MiSTerFin's little-endian BGRX8888 buffer to packed RGB."""
    expected = width * height * 4
    if len(frame) != expected:
        raise ValueError(f"expected {expected} frame bytes, got {len(frame)}")

    rgb = bytearray(width * height * 3)
    rgb[0::3] = frame[2::4]
    rgb[1::3] = frame[1::4]
    rgb[2::3] = frame[0::4]
    return bytes(rgb)


def kitty_chunks(control: str, payload: bytes = b"") -> Iterable[bytes]:
    """Encode one Kitty graphics command, splitting large payloads safely."""
    encoded = base64.b64encode(payload)
    if not encoded:
        yield ESC + b"_G" + control.encode("ascii") + b";" + ST
        return

    chunks = [encoded[i:i + 4096] for i in range(0, len(encoded), 4096)]
    for index, chunk in enumerate(chunks):
        more = int(index + 1 < len(chunks))
        prefix = f"{control},m={more}" if index == 0 else f"m={more}"
        yield ESC + b"_G" + prefix.encode("ascii") + b";" + chunk + ST


def display_cells(
    columns: int,
    rows: int,
    pixel_width: int = 0,
    pixel_height: int = 0,
) -> tuple[int, int]:
    """Fit MiSTerFin's non-square pixels into a physical 4:3 rectangle."""
    columns = max(columns, 1)
    rows = max(rows, 1)

    if pixel_width > 0 and pixel_height > 0:
        cell_width = pixel_width / columns
        cell_height = pixel_height / rows
    else:
        # A typical terminal cell is about twice as tall as it is wide.
        cell_width = 1.0
        cell_height = 2.0

    width_at_full_columns = columns * cell_width
    image_height_pixels = width_at_full_columns / DISPLAY_ASPECT
    fitted_rows = max(1, round(image_height_pixels / cell_height))

    if fitted_rows <= rows:
        return columns, fitted_rows

    height_at_full_rows = rows * cell_height
    image_width_pixels = height_at_full_rows * DISPLAY_ASPECT
    fitted_columns = max(1, round(image_width_pixels / cell_width))
    return min(fitted_columns, columns), rows


def terminal_geometry(tty: BinaryIO) -> tuple[int, int, int, int]:
    packed = fcntl.ioctl(tty.fileno(), termios.TIOCGWINSZ, b"\0" * 8)
    rows, columns, pixel_width, pixel_height = struct.unpack("HHHH", packed)
    fallback = shutil.get_terminal_size((80, 24))
    return columns or fallback.columns, rows or fallback.lines, pixel_width, pixel_height


def read_complete_frame(path: Path, expected_size: int) -> bytes | None:
    """Read a frame only if the producer did not change it during the read."""
    try:
        with path.open("rb") as source:
            before = os.fstat(source.fileno())
            if before.st_size != expected_size:
                return None
            frame = source.read()
            after = os.fstat(source.fileno())
    except FileNotFoundError:
        return None

    if len(frame) != expected_size:
        return None
    if before.st_size != after.st_size or before.st_mtime_ns != after.st_mtime_ns:
        return None
    return frame


class FrameWatch:
    """Wait for complete frame writes or atomic replacements on Linux."""

    CLOSE_WRITE = 0x00000008
    MOVED_TO = 0x00000080
    OVERFLOW = 0x00004000

    def __init__(self, path: Path):
        libc = C.CDLL(None, use_errno=True)
        init = libc.inotify_init1
        init.argtypes, init.restype = [C.c_int], C.c_int
        add = libc.inotify_add_watch
        add.argtypes, add.restype = [C.c_int, C.c_char_p, C.c_uint32], C.c_int
        self.fd = init(os.O_CLOEXEC | os.O_NONBLOCK)
        if self.fd < 0:
            raise OSError(C.get_errno(), "cannot watch terminal frames")
        self.name = os.fsencode(path.name)
        if add(self.fd, os.fsencode(path.parent), self.CLOSE_WRITE | self.MOVED_TO) < 0:
            error = C.get_errno()
            os.close(self.fd)
            self.fd = -1
            raise OSError(error, "cannot watch terminal frame directory")

    def __enter__(self):
        return self

    def __exit__(self, *_):
        if self.fd >= 0:
            os.close(self.fd)
            self.fd = -1

    def wait(self, timeout: float) -> bool:
        if not select.select([self.fd], [], [], timeout)[0]:
            return False
        events = os.read(self.fd, 65536)
        changed = False
        offset = 0
        while offset + 16 <= len(events):
            _, mask, _, size = struct.unpack_from("=iIII", events, offset)
            name = events[offset + 16:offset + 16 + size].rstrip(b"\0")
            changed = changed or name == self.name or bool(mask & self.OVERFLOW)
            offset += 16 + size
        return changed


class GhosttyPresenter:
    def __init__(self, tty: BinaryIO, width: int, height: int):
        self.tty = tty
        self.width = width
        self.height = height
        self.image_id: int | None = None

    def write(self, data: bytes) -> None:
        self.tty.write(data)

    def enter(self) -> None:
        self.write(b"\x1b[?1049h\x1b[?25l\x1b[2J\x1b[H")

    def leave(self) -> None:
        self.delete_image()
        self.write(b"\x1b[?25h\x1b[?1049l")

    def delete_image(self, image_id: int | None = None) -> None:
        image_id = self.image_id if image_id is None else image_id
        if image_id is None:
            return
        for chunk in kitty_chunks(f"a=d,d=I,i={image_id},q=2"):
            self.write(chunk)
        if image_id == self.image_id:
            self.image_id = None

    def create_image(self, image_id: int, rgb: bytes, columns: int, rows: int) -> None:
        self.write(b"\x1b[H")
        control = (
            f"a=T,f=24,t=d,s={self.width},v={self.height},i={image_id},"
            f"p={PLACEMENT_ID},c={columns},r={rows},C=1,q=2"
        )
        for chunk in kitty_chunks(control, rgb):
            self.write(chunk)
        self.image_id = image_id

    def replace_image(self, image_id: int, rgb: bytes, columns: int, rows: int) -> None:
        # Upload without displaying, place the complete new image over the old
        # one, then release the old image. There is never a blank placement.
        control = f"a=t,f=24,t=d,s={self.width},v={self.height},i={image_id},q=2"
        for chunk in kitty_chunks(control, rgb):
            self.write(chunk)

        placement = (
            f"a=p,i={image_id},p={PLACEMENT_ID},c={columns},r={rows},C=1,q=2"
        )
        old_image_id = self.image_id
        # Upload stays outside the synchronized update so Ghostty can keep
        # showing the old image. Commit placement and removal as one update.
        update = [SYNC_BEGIN, b"\x1b[H"]
        update.extend(kitty_chunks(placement))
        update.extend(kitty_chunks(f"a=d,d=I,i={old_image_id},q=2"))
        update.append(SYNC_END)
        self.write(b"".join(update))
        self.image_id = image_id

    def show(self, frame: bytes) -> None:
        columns, rows, pixel_width, pixel_height = terminal_geometry(self.tty)
        image_columns, image_rows = display_cells(
            columns,
            rows,
            pixel_width,
            pixel_height,
        )
        rgb = bgrx_to_rgb(frame, self.width, self.height)

        if self.image_id is None:
            self.create_image(IMAGE_IDS[0], rgb, image_columns, image_rows)
        else:
            next_image_id = IMAGE_IDS[1] if self.image_id == IMAGE_IDS[0] else IMAGE_IDS[0]
            self.replace_image(next_image_id, rgb, image_columns, image_rows)


def next_frame_deadline(previous: float, now: float, interval: float) -> float:
    """Keep the presentation clock steady and skip expired slots after a stall."""
    deadline = previous + interval
    if deadline <= now:
        deadline += (math.floor((now - deadline) / interval) + 1) * interval
    return deadline


def parse_args(argv: list[str]) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Navigate MiSTerFin inside Ghostty using the desktop harness."
    )
    mode = parser.add_mutually_exclusive_group()
    mode.add_argument("--ntsc", action="store_true", help="use the 640x240 layout")
    mode.add_argument("--pal", action="store_true", help="use the 640x288 layout (default)")
    parser.add_argument(
        "--fps",
        type=float,
        default=None,
        help=f"maximum terminal presentation rate (default: {DEFAULT_FPS:g}, or {VIDEO_FPS:g} with --inline-video)",
    )
    parser.add_argument(
        "--go",
        action="store_true",
        help="run the Go prototype (test frame unless --browse or --demo is selected)",
    )
    parser.add_argument("--inline-video", action="store_true", help="play video inside Ghostty using libmpv (requires --browse)")
    parser.add_argument("--browse", action="store_true", help="browse Jellyfin with the Go client")
    parser.add_argument("--demo", action="store_true", help="browse a local mock server with the Go client")
    parser.add_argument("--config", type=Path, help="Go Jellyfin configuration path")
    parser.add_argument("--state-dir", type=Path, help="Go session directory")
    parser.add_argument(
        "--binary",
        type=Path,
        help="override the selected C or Go host binary",
    )
    parser.add_argument("--no-build", action="store_true", help="do not run make first")
    parser.add_argument(
        "--log",
        type=Path,
        default=Path("/tmp/misterfin-ghostty.log"),
        help="child stdout and stderr log",
    )
    parser.add_argument(
        "--force",
        action="store_true",
        help="run even when TERM does not identify Ghostty",
    )
    args = parser.parse_args(argv)
    if args.fps is None:
        args.fps = VIDEO_FPS if args.inline_video else DEFAULT_FPS
    if args.inline_video and (not args.browse or args.demo):
        parser.error("--inline-video requires --browse with a real Jellyfin server")
    if not math.isfinite(args.fps) or args.fps <= 0:
        parser.error("--fps must be finite and greater than zero")
    if args.demo:
        if args.config or args.state_dir:
            parser.error("--demo uses temporary configuration and session files")
        args.browse = True
    if args.browse:
        args.go = True
    if (args.config or args.state_dir) and not args.browse:
        parser.error("--config and --state-dir require --browse")
    if args.binary is None:
        args.binary = REPO_ROOT / ("build/misterfin-go" if args.go else "misterfin")
    return args


def start_demo(directory: Path, cleanup: ExitStack) -> Path:
    """Reuse the C harness's mock Jellyfin data on an ephemeral loopback port."""
    spec = importlib.util.spec_from_file_location("mock_jellyfin", REPO_ROOT / "tools/mock-jellyfin.py")
    assert spec and spec.loader
    mock = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mock)

    class QuietHandler(mock.Handler):
        def log_message(self, *_args):
            pass

        def do_GET(self):
            try:
                super().do_GET()
            except (BrokenPipeError, ConnectionResetError):
                pass  # Navigating away can cancel an in-flight artwork request.

    server = ThreadingHTTPServer(("127.0.0.1", 0), QuietHandler)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    cleanup.callback(server.server_close)
    cleanup.callback(thread.join)
    cleanup.callback(server.shutdown)
    config = directory / "jellyfin.conf"
    config.write_text(f"http://127.0.0.1:{server.server_port}\nmock-api-key\nmockuser\n")
    return config


def stop_process(process: subprocess.Popen[bytes], timeout: float = 2) -> None:
    if process.poll() is not None:
        return
    process.terminate()
    try:
        process.wait(timeout=timeout)
    except subprocess.TimeoutExpired:
        process.kill()
        process.wait()


def child_environment(width: int, height: int, frame_path: Path) -> dict[str, str]:
    env = os.environ.copy()
    env["MISTERFIN_FB"] = f"{width}x{height}"
    env["MISTERFIN_FRAME_OUT"] = str(frame_path)
    env["MISTERFIN_STDIN"] = "1"
    env.setdefault("MISTERFIN_CACHE_ROOT", "/tmp/misterfin-cache")
    env.pop("MISTERFIN_KEYS", None)
    env.pop("MISTERFIN_KEYS_HOLD", None)
    return env


def run(args: argparse.Namespace) -> int:
    if "ghostty" not in os.environ.get("TERM", "").lower() and not args.force:
        print("This viewer requires Ghostty (expected TERM=xterm-ghostty).", file=sys.stderr)
        print("Use --force only with another Kitty-graphics-compatible terminal.", file=sys.stderr)
        return 2

    if not sys.stdin.isatty():
        print("Interactive MiSTerFin input requires a terminal on stdin.", file=sys.stderr)
        return 2

    if not args.no_build:
        build = ["make", "--no-print-directory"]
        if args.go:
            build += ["-f", "Makefile.port", "host"]
        completed = subprocess.run(build, cwd=REPO_ROOT)
        if completed.returncode:
            return completed.returncode

    binary = args.binary.resolve()
    if not binary.is_file():
        print(f"MiSTerFin host binary not found: {binary}", file=sys.stderr)
        return 2

    width = 640
    height = 240 if args.ntsc else 288
    frame_size = width * height * 4
    frame_interval = 1.0 / args.fps
    args.log.parent.mkdir(parents=True, exist_ok=True)

    process: subprocess.Popen[bytes] | None = None
    stopping = False

    def request_stop(_signum: int, _frame: object) -> None:
        nonlocal stopping
        stopping = True

    previous_handlers = {
        signum: signal.signal(signum, request_stop)
        for signum in (signal.SIGINT, signal.SIGTERM, signal.SIGHUP)
    }

    try:
        with tempfile.TemporaryDirectory(prefix="misterfin-ghostty-") as temp_dir, ExitStack() as cleanup:
            frame_path = Path(temp_dir) / "frame.raw"
            env = child_environment(width, height, frame_path)
            command = [str(binary)]
            if args.browse:
                command += ["-browse", "-audio-player", str(Path(__file__).with_name("video_player.py").resolve())]
                if args.inline_video:
                    command += ["-terminal-player", str(Path(__file__).with_name("video_player.py").resolve())]
                config, state_dir = args.config, args.state_dir
                if args.demo:
                    config = start_demo(Path(temp_dir), cleanup)
                    state_dir = Path(temp_dir) / "session"
                if config:
                    command += ["-config", str(config.resolve())]
                if state_dir:
                    command += ["-state-dir", str(state_dir.resolve())]
            elif args.go:
                command.append("-wait")

            with args.log.open("wb") as log, open("/dev/tty", "wb", buffering=0) as tty, FrameWatch(frame_path) as frame_watch:
                presenter = GhosttyPresenter(tty, width, height)
                presenter.enter()
                try:
                    process = subprocess.Popen(
                        command,
                        cwd=REPO_ROOT,
                        env=env,
                        stdin=None,
                        stdout=log,
                        stderr=log,
                    )
                    previous_frame: bytes | None = None
                    next_frame_at = time.monotonic()
                    pending_frame = True

                    while process.poll() is None and not stopping:
                        now = time.monotonic()
                        timeout = min(max(0.0, next_frame_at - now), 0.05) if pending_frame else 0.05
                        pending_frame = frame_watch.wait(timeout) or pending_frame
                        now = time.monotonic()
                        if not pending_frame or now < next_frame_at:
                            continue

                        pending_frame = False
                        frame = read_complete_frame(frame_path, frame_size)
                        if frame is not None and frame != previous_frame:
                            presenter.show(frame)
                            previous_frame = frame
                            # Limit uploads from their start time. An arriving
                            # video frame need not wait for an unrelated tick.
                            next_frame_at = next_frame_deadline(now, time.monotonic(), frame_interval)
                finally:
                    if process is not None:
                        # Allow Go to finish bounded Live TV negotiation and tuner cleanup.
                        stop_process(process, timeout=30 if args.browse else 2)
                    presenter.leave()
    finally:
        for signum, handler in previous_handlers.items():
            signal.signal(signum, handler)

    assert process is not None
    if process.returncode not in (0, -signal.SIGTERM):
        print(f"MiSTerFin exited with status {process.returncode}. See {args.log}.", file=sys.stderr)
        return process.returncode or 1
    return 0


def main() -> int:
    return run(parse_args(sys.argv[1:]))


if __name__ == "__main__":
    raise SystemExit(main())
