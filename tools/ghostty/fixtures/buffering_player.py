"""Publish frames and buffering feedback when the test advances a stage file."""

import argparse
from pathlib import Path
import time


def main():
    parser = argparse.ArgumentParser()
    for name in ("controls", "status"):
        parser.add_argument("--" + name, action="store_true")
    for name in ("output", "width", "height"):
        parser.add_argument("--" + name)
    args = parser.parse_args()
    output = Path(args.output)
    stage = output.parent / "stage"
    for number in range(1, 4):
        while not stage.exists() or stage.read_text() != str(number):
            time.sleep(.02)
        output.write_bytes(bytes([23]) * 640 * 240 * 4)
        print("ANS_BUFFERING=" + ("true" if number == 2 else "false"), flush=True)
        print("ANS_TIME_POSITION=" + str(number + 1), flush=True)
    time.sleep(30)


if __name__ == "__main__":
    main()
