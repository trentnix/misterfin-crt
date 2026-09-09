#!/bin/sh
set -eu
exec "${ZIG:-zig}" cc -target arm-linux-gnueabihf.2.31 -mcpu=cortex_a9 "$@"
