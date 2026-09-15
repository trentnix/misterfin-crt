#!/bin/bash
set -e

PREFIX=/build/sysroot
mkdir -p $PREFIX

# pkg-config must search our sysroot
export PKG_CONFIG=pkg-config
export PKG_CONFIG_PATH=$PREFIX/lib/pkgconfig
export PKG_CONFIG_LIBDIR=$PREFIX/lib/pkgconfig

MPLAYER_VER=1.5

MPLAYER_SHA256=650cd55bb3cb44c9b39ce36dac488428559799c5f18d16d98edb2b7256cbbf85

# ── MPlayer ──────────────────────────────────────────────────────────────────
# Video uses a Jellyfin transcode through a pipe. Music uses the original
# stream through the local Go proxy. Neither needs physical disc support.
echo "=== Building MPlayer $MPLAYER_VER ==="
if [ -f /build/MPlayer-source.tar.xz ]; then
    cp /build/MPlayer-source.tar.xz MPlayer-$MPLAYER_VER.tar.xz
else
    wget -q https://mplayerhq.hu/MPlayer/releases/MPlayer-$MPLAYER_VER.tar.xz
fi
echo "$MPLAYER_SHA256  MPlayer-$MPLAYER_VER.tar.xz" | sha256sum -c -
tar xf MPlayer-$MPLAYER_VER.tar.xz
# Apply vsync patch: wait for blanking interval before each frame write to
# eliminate tearing. The flag file, /tmp/misterdvd_vsync, retains the C
# client's path for compatibility with the output driver.
cp /build/vo_fbdev.c MPlayer-$MPLAYER_VER/libvo/vo_fbdev.c
# The session-message banner in vo_fbdev.c renders text with the app's own
# 8x8 font. Keep the header in docker/ so the build context is self-contained.
cp /build/font8x8.h MPlayer-$MPLAYER_VER/libvo/font8x8.h
cd MPlayer-$MPLAYER_VER

./configure \
    --target=arm-linux-gnueabihf \
    --cc=arm-linux-gnueabihf-gcc \
    --as=arm-linux-gnueabihf-gcc \
    --ar=arm-linux-gnueabihf-ar \
    --nm=arm-linux-gnueabihf-nm \
    --ranlib=arm-linux-gnueabihf-ranlib \
    --prefix=$PREFIX \
    --extra-cflags="-march=armv7-a -mfpu=neon -mfloat-abi=hard -O2 -I$PREFIX/include" \
    --extra-ldflags="-L$PREFIX/lib" \
    --disable-mencoder \
    --disable-x11 \
    --disable-xv \
    --disable-xvmc \
    --disable-gl \
    --disable-sdl \
    --disable-gui \
    --disable-png \
    --disable-jpeg \
    --disable-gif \
    --disable-lirc \
    --disable-lircc \
    --disable-joystick \
    --disable-tv \
    --disable-pvr \
    --disable-radio \
    --disable-ossaudio \
    --disable-arts \
    --disable-esd \
    --disable-nas \
    --disable-openal \
    --disable-jack \
    --disable-ladspa \
    --disable-libdv \
    --disable-speex \
    --disable-theora \
    --disable-toolame \
    --disable-twolame \
    --disable-dvdnav \
    --disable-dvdread \
    --enable-fbdev \
    --enable-alsa

make -j$(nproc)

arm-linux-gnueabihf-strip mplayer
cp mplayer /build/mplayer-arm
if [ -n "${OUTPUT_DIR:-}" ]; then
    cp mplayer "$OUTPUT_DIR/misterfin-crt-mplayer-arm"
    cp ../MPlayer-$MPLAYER_VER.tar.xz "$OUTPUT_DIR/misterfin-crt-mplayer-source.tar.xz"
    {
        echo "MPlayer source: $MPLAYER_VER"
        echo "MPlayer source SHA256: $MPLAYER_SHA256"
        printf 'Bundled FFmpeg: '
        cat ffmpeg/RELEASE
        arm-linux-gnueabihf-gcc --version | head -1
    } > "$OUTPUT_DIR/misterfin-crt-mplayer-build.txt"
fi
echo "=== Done: /build/mplayer-arm ==="
