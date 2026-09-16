#!/usr/bin/env python3
"""Extract the pinned Fusion Pixel BDF into fixed 8x8 fallback glyph records."""

import argparse
import hashlib
from pathlib import Path
import struct
import zipfile

# Input: fusion-pixel-font-8px-monospaced-bdf-v2026.09.01.zip from upstream.
ARCHIVE_SHA256 = "c2e1fca87eafb0f5dbae64378a8a73f85499479ce67616e6eaa618f85c55f626"


def convert(archive: Path) -> bytes:
    """Preserve glyph pixels, center half-width cells, and omit Latin-1."""
    data = archive.read_bytes()
    if hashlib.sha256(data).hexdigest() != ARCHIVE_SHA256:
        raise ValueError("font archive does not match the pinned release")
    with zipfile.ZipFile(archive) as source:
        lines = source.read("fusion-pixel-8px-monospaced-latin.bdf").decode().splitlines()
    glyphs = {}
    for block in "\n".join(lines).split("STARTCHAR ")[1:]:
        fields = block.splitlines()
        code = int(next(line for line in fields if line.startswith("ENCODING ")).split()[1])
        if code <= 255:
            continue
        advance = int(next(line for line in fields if line.startswith("DWIDTH ")).split()[1])
        width, height, left, bottom = map(int, next(line for line in fields if line.startswith("BBX ")).split()[1:])
        left += (8 - advance) // 2
        top = 7 - bottom - height
        if not (0 <= left <= left + width <= 8 and 0 <= top <= top + height <= 8):
            raise ValueError(f"glyph U+{code:04X} does not fit an 8x8 cell")
        bitmap = fields.index("BITMAP") + 1
        rows = bytearray(8)
        for y, line in enumerate(fields[bitmap:bitmap + height]):
            bits = int(line, 16)
            for x in range(width):
                if bits & (1 << (7 - x)):
                    rows[top + y] |= 1 << (left + x)
        glyphs[code] = rows
    return b"".join(struct.pack("<I", code) + glyphs[code] for code in sorted(glyphs))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("archive", type=Path)
    args = parser.parse_args()
    output = Path(__file__).resolve().parents[1] / "internal/ui/fonts/unicode.bin"
    data = convert(args.archive)
    output.write_bytes(data)
    print(f"{len(data) // 12} glyphs, {len(data)} bytes: {output}")


if __name__ == "__main__":
    main()
