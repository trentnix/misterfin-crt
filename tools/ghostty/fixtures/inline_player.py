"""Publish a fixed frame and acknowledge picture changes without decoding media."""

import argparse
from pathlib import Path
import select
import sys
import time


def record_picture(output, zoom):
    """Record launch and live picture choices for browser assertions."""
    with (output.parent / "picture-modes").open("a") as log:
        log.write(str(zoom) + "\n")


def main():
    parser = argparse.ArgumentParser()
    for name in ("controls", "status", "zoom-4-3", "captions"):
        parser.add_argument("--" + name, action="store_true")
    for name in ("output", "width", "height"):
        parser.add_argument("--" + name)
    args = parser.parse_args()
    output = Path(args.output)
    record_picture(output, args.zoom_4_3)
    output.write_bytes(bytes([23]) * 640 * 240 * 4)
    print("ANS_TIME_POSITION=2", flush=True)
    if args.captions:
        print("ANS_CAPTION_TEXT=" + b"Live caption text".hex(), flush=True)
    deadline = time.monotonic() + 30
    while time.monotonic() < deadline:
        if not select.select([sys.stdin], [], [], .05)[0]:
            continue
        parts = sys.stdin.readline().split()
        if len(parts) == 3 and parts[0] == "picture":
            record_picture(output, parts[1] == "1")
            print("ANS_PICTURE_MODE=" + parts[2] + "," + parts[1], flush=True)


if __name__ == "__main__":
    main()
