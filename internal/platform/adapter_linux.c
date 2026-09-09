// Framebuffer geometry and presentation derived from MiSTerFin src/fb.c.
// Copyright © 2026 Pudding Studio. Licensed under CC BY-NC 4.0.
//go:build linux && cgo

#include "adapter.h"
#include <errno.h>
#include <fcntl.h>
#include <linux/fb.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/ioctl.h>
#include <sys/mman.h>
#include <unistd.h>

struct mf_display {
    int fd, w, h, ow, oh, stride, bx, by, bw, bh;
    size_t size;
    uint8_t *mem, *saved;
};

int mf_open(mf_display **out, const char *device, int width, int height)
{
    *out = NULL;
    mf_display *d = calloc(1, sizeof(*d));
    if (!d) return ENOMEM;
    d->fd = -1;
    int error = EINVAL;
    if (!width && !height) {
        d->fd = open(device, O_RDWR | O_CLOEXEC);
        if (d->fd < 0) { error = errno; goto fail; }
        struct fb_var_screeninfo v;
        struct fb_fix_screeninfo f;
        if (ioctl(d->fd, FBIOGET_VSCREENINFO, &v) < 0 ||
            ioctl(d->fd, FBIOGET_FSCREENINFO, &f) < 0) {
            error = errno; goto fail;
        }
        /* Reject layouts that would make the BGRX copy unsafe or incorrect. */
        if (v.bits_per_pixel != 32 || v.red.offset != 16 ||
            v.green.offset != 8 || v.blue.offset != 0 ||
            v.red.length != 8 || v.green.length != 8 || v.blue.length != 8 ||
            v.red.msb_right || v.green.msb_right || v.blue.msb_right ||
            (v.transp.length && (v.transp.length != 8 || v.transp.offset != 24 || v.transp.msb_right)) ||
            v.nonstd || v.grayscale || v.xoffset || v.yoffset ||
            f.type != FB_TYPE_PACKED_PIXELS || f.visual != FB_VISUAL_TRUECOLOR ||
            v.xres < 1 || v.xres > 8192 || v.yres < 1 || v.yres > 8192 ||
            f.line_length < v.xres * 4 || f.line_length > 32768 ||
            (uint64_t)f.line_length * v.yres > f.smem_len) {
            error = ENOTSUP; goto fail;
        }
        width = v.xres; height = v.yres; d->stride = f.line_length;
    }
    if (width < 1 || width > 8192 || height < 1 || height > 8192) goto fail;
    if (!d->stride) d->stride = width * 4;
    d->size = (size_t)d->stride * height;
    if (d->size > 128u * 1024u * 1024u) goto fail;
    d->ow = d->w = width; d->oh = d->h = height;
    d->bw = width; d->bh = height;
    if (height == 480 || height == 576) {
        d->h /= 2;
    } else if (width > 640 || height > 480 ||
               (height >= 360 && 3 * width > 4 * height)) {
        d->bw = height * 4 / 3;
        if (d->bw > width) {
            d->bw = width;
            d->bh = width * 3 / 4;
        }
        d->bx = (width - d->bw) / 2; d->by = (height - d->bh) / 2;
        d->w = d->bw >= 640 ? 640 : d->bw;
        d->h = d->bw >= 640 ? 288 : d->bh * 2 / 3;
    }
    if (!d->w || !d->h) goto fail;
    if (d->fd < 0) {
        d->mem = calloc(1, d->size);
        if (!d->mem) { error = ENOMEM; goto fail; }
    } else {
        d->mem = mmap(NULL, d->size, PROT_READ | PROT_WRITE, MAP_SHARED, d->fd, 0);
        if (d->mem == MAP_FAILED) { d->mem = NULL; error = errno; goto fail; }
        d->saved = malloc(d->size);
        if (!d->saved) { error = ENOMEM; goto fail; }
        memcpy(d->saved, d->mem, d->size);
    }
    *out = d;
    return 0;
fail:
    mf_close(d);
    return error;
}

void mf_geometry(mf_display *d, int *w, int *h, int *ow, int *oh)
{
    *w = d->w; *h = d->h; *ow = d->ow; *oh = d->oh;
}

int mf_present(mf_display *d, const uint8_t *pixels, size_t size)
{
    if (!pixels || size != (size_t)d->w * d->h * 4) return EINVAL;
    if (d->fd >= 0) {
        uint32_t dummy = 0;
        if (ioctl(d->fd, FBIO_WAITFORVSYNC, &dummy) < 0) return errno;
    }
    /* Clear only the bars and padding. Avoid a full black pass before the copy. */
    memset(d->mem, 0, (size_t)d->by * d->stride);
    memset(d->mem + (size_t)(d->by + d->bh) * d->stride, 0,
           (size_t)(d->oh - d->by - d->bh) * d->stride);
    for (int y = 0; y < d->bh; y++) {
        const uint8_t *src = pixels + (size_t)(y * d->h / d->bh) * d->w * 4;
        uint8_t *row = d->mem + (size_t)(d->by + y) * d->stride;
        memset(row, 0, (size_t)d->bx * 4);
        memset(row + (d->bx + d->bw) * 4, 0,
               d->stride - (size_t)(d->bx + d->bw) * 4);
        uint8_t *dst = row + d->bx * 4;
        if (d->bw == d->w) memcpy(dst, src, (size_t)d->w * 4);
        else for (int x = 0; x < d->bw; x++)
            memcpy(dst + x * 4, src + (x * d->w / d->bw) * 4, 4);
    }
    return 0;
}

int mf_dump(mf_display *d, const char *path)
{
    if (d->fd >= 0) return EINVAL;
    FILE *f = fopen(path, "wb");
    if (!f) return errno;
    int error = fwrite(d->mem, 1, d->size, f) == d->size ? 0 : EIO;
    if (fclose(f) && !error) error = errno;
    return error;
}

int mf_close(mf_display *d)
{
    if (!d) return 0;
    int error = 0;
    if (d->mem) {
        if (d->fd < 0) free(d->mem);
        else {
            if (d->saved) memcpy(d->mem, d->saved, d->size);
            if (munmap(d->mem, d->size)) error = errno;
        }
    }
    free(d->saved);
    if (d->fd >= 0 && close(d->fd) && !error) error = errno;
    free(d);
    return error;
}
