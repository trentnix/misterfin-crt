#!/usr/bin/env python3
"""Render libmpv video into the Ghostty harness's BGRX frame file.

Media arrives on inherited descriptor 3. libmpv owns audio and presentation
timing. A dedicated render thread publishes complete frames atomically.
The FFI declarations follow libmpv's public client.h and render.h APIs.
"""

import argparse
import ctypes as C
import ctypes.util
import math
import os
from pathlib import Path
import signal
import select
import sys
from urllib.parse import urlparse
import tempfile
import threading
import time


class RenderParam(C.Structure):
    _fields_ = [("type", C.c_int), ("data", C.c_void_p)]


class Event(C.Structure):
    _fields_ = [("event_id", C.c_int), ("error", C.c_int),
                ("reply_userdata", C.c_uint64), ("data", C.c_void_p)]


class EndFile(C.Structure):
    _fields_ = [("reason", C.c_int), ("error", C.c_int)]


def bind(lib, name, restype, *args):
    fn = getattr(lib, name)
    fn.restype, fn.argtypes = restype, args
    return fn


class Node(C.Structure):
    pass


class NodeList(C.Structure):
    _fields_ = [("num", C.c_int), ("values", C.POINTER(Node)),
                ("keys", C.POINTER(C.c_char_p))]


class NodeValue(C.Union):
    _fields_ = [("string", C.c_char_p), ("flag", C.c_int),
                ("int64", C.c_int64), ("double", C.c_double),
                ("list", C.POINTER(NodeList))]


Node._fields_ = [("u", NodeValue), ("format", C.c_int)]


class MPV:
    def __init__(self):
        self.lib = C.CDLL(ctypes.util.find_library("mpv") or "libmpv.so.2")
        self.create = bind(self.lib, "mpv_create", C.c_void_p)
        self.option = bind(self.lib, "mpv_set_option_string", C.c_int, C.c_void_p, C.c_char_p, C.c_char_p)
        self.initialize = bind(self.lib, "mpv_initialize", C.c_int, C.c_void_p)
        self.command = bind(self.lib, "mpv_command", C.c_int, C.c_void_p, C.POINTER(C.c_char_p))
        self.wait = bind(self.lib, "mpv_wait_event", C.POINTER(Event), C.c_void_p, C.c_double)
        self.property = bind(self.lib, "mpv_get_property", C.c_int, C.c_void_p, C.c_char_p, C.c_int, C.c_void_p)
        self.free_node = bind(self.lib, "mpv_free_node_contents", None, C.POINTER(Node))
        self.destroy = bind(self.lib, "mpv_terminate_destroy", None, C.c_void_p)
        self.render_create = bind(self.lib, "mpv_render_context_create", C.c_int, C.POINTER(C.c_void_p), C.c_void_p, C.POINTER(RenderParam))
        self.callback_type = C.CFUNCTYPE(None, C.c_void_p)
        self.callback = bind(self.lib, "mpv_render_context_set_update_callback", None, C.c_void_p, self.callback_type, C.c_void_p)
        self.update = bind(self.lib, "mpv_render_context_update", C.c_uint64, C.c_void_p)
        self.render = bind(self.lib, "mpv_render_context_render", C.c_int, C.c_void_p, C.POINTER(RenderParam))
        self.free = bind(self.lib, "mpv_render_context_free", None, C.c_void_p)

    def audio_levels(self, handle):
        """Read a complete metadata snapshot through libmpv's node API."""
        node = Node()
        if self.property(handle, b"af-metadata/music", 6, C.byref(node)) < 0:
            return (0.0, 0.0)
        values = {}
        try:
            if node.format == 8 and node.u.list:
                entries = node.u.list.contents
                for i in range(entries.num):
                    value = entries.values[i]
                    if value.format == 1 and value.u.string:
                        values[entries.keys[i]] = value.u.string
        finally:
            self.free_node(C.byref(node))
        levels = []
        for channel in (1, 2):
            key = f"lavfi.astats.{channel}.RMS_level".encode()
            try:
                db = float(values.get(key, values.get(b"lavfi.astats.1.RMS_level", b"-inf")))
                value = min(1.0, 10 ** (min(0.0, db) / 20)) if math.isfinite(db) else 0.0
            except ValueError:
                value = 0.0
            levels.append(value)
        return levels

    def caption_text(self, handle):
        """Read decoded caption text without letting libmpv draw it into video."""
        node = Node()
        if self.property(handle, b"sub-text", 6, C.byref(node)) < 0:
            return ""
        try:
            if node.format == 1 and node.u.string:
                return node.u.string.decode("utf-8", errors="replace")
            return ""
        finally:
            self.free_node(C.byref(node))

    def send(self, handle, *args):
        values = (C.c_char_p * (len(args) + 1))(*(arg.encode() for arg in args), None)
        return self.command(handle, values)


def publish_frame(output, source, width, height):
    # Render at square-pixel 4:3, then sample the logical CRT rows. This keeps
    # both the source aspect ratio and the harness's tall-pixel correction.
    row_bytes = width * 4
    frame = b"".join(source[(y * 480 // height) * row_bytes:
                            (y * 480 // height + 1) * row_bytes] for y in range(height))
    fd, path = tempfile.mkstemp(prefix=".video-frame-", dir=output.parent)
    try:
        with os.fdopen(fd, "wb") as target:
            target.write(frame)
        os.replace(path, output)
    finally:
        if os.path.exists(path):
            os.unlink(path)


def set_picture(mpv, handle, mode):
    """Change the current frame's fit without seeking or changing pause state."""
    if mode not in (0, 1):
        return False
    if mpv.send(handle, "set", "panscan", "0") < 0:
        return False
    if mode == 0:
        return mpv.send(handle, "set", "video-zoom", "0") >= 0

    # video-zoom is logarithmic. Scale enough to fill the 4:3 surface for
    # wide or narrow pictures. A 4:3 source gets a fixed enlargement so Zoom
    # can remove letterboxing encoded within the video frame.
    aspect = C.c_double()
    if mpv.property(handle, b"video-out-params/aspect", 5, C.byref(aspect)) < 0:
        return False
    if not math.isfinite(aspect.value) or aspect.value <= 0:
        return False
    target = 4 / 3
    if abs(aspect.value - target) <= 0.01:
        factor = 4 / 3
    else:
        factor = max(aspect.value / target, target / aspect.value)
    return mpv.send(handle, "set", "video-zoom", f"{math.log2(factor):.9f}") >= 0


def play(output, width, height, audio="auto", audio_only=False, source="fd://3", controls=False, status=False, audio_levels=False, zoom_4_3=False, captions=False):
    mpv = MPV()
    handle = mpv.create()
    if not handle:
        raise RuntimeError("cannot create libmpv player")
    ready, stop, wake = threading.Event(), threading.Event(), threading.Event()
    errors = []
    frames = [0]
    worker = None

    def render_video():
        context = C.c_void_p()
        try:
            api = C.create_string_buffer(b"sw")
            params = (RenderParam * 2)(RenderParam(1, C.addressof(api)), RenderParam(0, None))
            if mpv.render_create(C.byref(context), handle, params) < 0:
                raise RuntimeError("cannot create software video renderer")
            callback = mpv.callback_type(lambda _: wake.set())
            mpv.callback(context, callback, None)
            size = (C.c_int * 2)(width, 480)
            stride = C.c_size_t(width * 4)
            storage = C.create_string_buffer(width * 480 * 4 + 63)
            pointer = (C.addressof(storage) + 63) & ~63
            pixels = (C.c_ubyte * (width * 480 * 4)).from_address(pointer)
            format_name = C.create_string_buffer(b"bgr0")
            params = (RenderParam * 5)(
                RenderParam(17, C.addressof(size)), RenderParam(18, C.addressof(format_name)),
                RenderParam(19, C.addressof(stride)), RenderParam(20, pointer), RenderParam(0, None))
            ready.set()
            while not stop.is_set():
                wake.wait(0.1)
                wake.clear()
                if mpv.update(context) & 1:
                    if mpv.render(context, params) < 0:
                        raise RuntimeError("video frame rendering failed")
                    publish_frame(output, bytes(pixels), width, height)
                    frames[0] += 1
        except Exception as error:
            errors.append(error)
            ready.set()
            stop.set()
        finally:
            if context.value:
                mpv.free(context)

    reported = False
    previous = {}
    try:
        for key, value in {"config": "no", "terminal": "no", "msg-level": "all=no",
                           "vo": "libmpv", "idle": "yes", "hwdec": "no",
                           "input-default-bindings": "no", "input-terminal": "no",
                           "volume": "71"}.items():
            if mpv.option(handle, key.encode(), value.encode()) < 0:
                raise RuntimeError("unsupported video player option")
        if audio != "auto" and mpv.option(handle, b"ao", audio.encode()) < 0:
            raise RuntimeError("unsupported audio output")
        if zoom_4_3 and not audio_only and mpv.option(handle, b"video-zoom", f"{math.log2(4 / 3):.9f}".encode()) < 0:
            raise RuntimeError("cannot enable picture zoom")
        if status:
            # Descriptor 3 is a pipe, so network cache auto-detection may not apply.
            for key in (b"cache", b"cache-pause"):
                if mpv.option(handle, key, b"yes") < 0:
                    raise RuntimeError("cannot enable playback buffering")
        if audio_only and mpv.option(handle, b"vid", b"no") < 0:
            raise RuntimeError("cannot disable video")
        meter_enabled = audio_only and audio_levels and mpv.option(handle, b"af", b"@music:lavfi=[astats=metadata=1:reset=1:measure_perchannel=RMS_level:measure_overall=none]") >= 0
        if captions and not audio_only:
            for key, value in {"sub-create-cc-track": "yes", "sub-visibility": "no", "sid": "1"}.items():
                if mpv.option(handle, key.encode(), value.encode()) < 0:
                    raise RuntimeError("cannot enable closed-caption extraction")
        if mpv.initialize(handle) < 0:
            raise RuntimeError("cannot initialize libmpv")
        if not audio_only:
            worker = threading.Thread(target=render_video, name="video-render")
            worker.start()
            if not ready.wait(5) or errors:
                raise RuntimeError("video renderer did not initialize")
        for sig in (signal.SIGINT, signal.SIGTERM):
            previous[sig] = signal.signal(sig, lambda *_: stop.set())
        if mpv.send(handle, "loadfile", source, "replace") < 0:
            raise RuntimeError("cannot open media pipe")
        last_caption = None
        next_levels = 0.0
        next_report = 0.0
        control_buffer = b""
        control_open = controls or (audio_only and source != "fd://3")
        picture_ready = not zoom_4_3 or audio_only
        while not stop.is_set():
            if not picture_ready:
                picture_ready = set_picture(mpv, handle, 1)
                if picture_ready:
                    wake.set()
            if control_open and select.select([sys.stdin], [], [], 0)[0]:
                chunk = os.read(sys.stdin.fileno(), 1024)
                if not chunk:
                    control_open = False
                control_buffer += chunk
                while b"\n" in control_buffer:
                    line, control_buffer = control_buffer.split(b"\n", 1)
                    parts = line.decode("ascii", errors="ignore").split()
                    if len(parts) == 2 and parts[0] == "pause" and parts[1] in ("true", "false"):
                        mpv.send(handle, "set", "pause", "yes" if parts[1] == "true" else "no")
                    elif audio_only and len(parts) == 2 and parts[0] == "seek" and parts[1] in ("-10", "10"):
                        mpv.send(handle, "seek", parts[1], "relative+exact")
                    elif not audio_only and len(parts) == 3 and parts[0] == "picture" and parts[1] in ("0", "1") and parts[2].isdecimal():
                        request = int(parts[2])
                        if 0 < request < 2**31:
                            mode = int(parts[1])
                            applied = mode if set_picture(mpv, handle, mode) else -1
                            print(f"ANS_PICTURE_MODE={request},{applied}", flush=True)
                    next_report = 0.0
                if len(control_buffer) > 1024:
                    control_buffer = b""
            event = mpv.wait(handle, 0.05).contents
            if event.event_id == 7:  # MPV_EVENT_END_FILE
                end = C.cast(event.data, C.POINTER(EndFile)).contents
                if end.reason == 4 or end.error < 0:
                    raise RuntimeError("video decoding failed")
                break
            if captions and not audio_only:
                text = mpv.caption_text(handle)
                if text != last_caption:
                    # Send screen snapshots, including clears. The shared UI
                    # decides whether to show them. Bound each UTF-8 payload.
                    encoded = text.encode("utf-8")[:2048].decode("utf-8", errors="ignore").encode("utf-8")
                    print("ANS_CAPTION_TEXT=" + encoded.hex(), flush=True)
                    last_caption = text
            now = time.monotonic()
            if meter_enabled and now >= next_levels:
                left, right = mpv.audio_levels(handle)
                print(f"ANS_AUDIO_LEVELS={left:.6f},{right:.6f}", flush=True)
                next_levels = now + 0.05
            if now >= next_report:
                if status:
                    buffering = C.c_int()
                    if mpv.property(handle, b"paused-for-cache", 3, C.byref(buffering)) >= 0:
                        print(f"ANS_BUFFERING={'true' if buffering.value else 'false'}", flush=True)
                next_report = now + 0.25
                if not (frames[0] or audio_only):
                    continue
                position = C.c_double()
                if mpv.property(handle, b"time-pos", 5, C.byref(position)) >= 0 and math.isfinite(position.value):
                    reported = True
                    print(f"ANS_TIME_POSITION={max(0, position.value):.3f}", flush=True)
        if errors:
            raise RuntimeError("video renderer failed")
        if not reported and not stop.is_set():
            raise RuntimeError("no video frames decoded")
    finally:
        # Stop media and free the render context before destroying the core.
        mpv.send(handle, "stop")
        stop.set()
        wake.set()
        if worker:
            worker.join()
        mpv.destroy(handle)
        for sig, handler in previous.items():
            signal.signal(sig, handler)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true")
    parser.add_argument("--audio-only", action="store_true")
    parser.add_argument("--audio-levels", action="store_true")
    parser.add_argument("--zoom-4-3", action="store_true")
    parser.add_argument("--captions", action="store_true", help="publish embedded CC text for the shared overlay")
    parser.add_argument("--source", default="fd://3")
    parser.add_argument("--controls", action="store_true")
    parser.add_argument("--status", action="store_true")
    parser.add_argument("--output", type=Path)
    parser.add_argument("--width", type=int, default=640)
    parser.add_argument("--height", type=int, choices=(240, 288), default=240)
    parser.add_argument("--audio", default="auto")
    args = parser.parse_args()
    if args.check:
        MPV()
        print("libmpv software rendering API is available")
        return
    if not args.audio_only and (args.output is None or args.width != 640):
        parser.error("--output and a 640-pixel framebuffer are required")
    if args.source != "fd://3":
        source = urlparse(args.source)
        if not args.audio_only or source.scheme != "http" or source.hostname != "127.0.0.1" or source.username or source.password or source.query or source.fragment:
            parser.error("--source must identify the local audio proxy")
    play(args.output, args.width, args.height, args.audio, args.audio_only, args.source, args.controls, args.status, args.audio_levels, args.zoom_4_3, args.captions)


if __name__ == "__main__":
    try:
        main()
    except (OSError, RuntimeError):
        # Never forward libmpv's media diagnostics, which can include metadata.
        raise SystemExit("Terminal video failed. Check libmpv and audio availability.")
