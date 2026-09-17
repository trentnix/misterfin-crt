/*
 * This file is part of MPlayer.
 *
 * MPlayer is free software; you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation; either version 2 of the License, or
 * (at your option) any later version.
 *
 * MPlayer is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License along
 * with MPlayer; if not, write to the Free Software Foundation, Inc.,
 * 51 Franklin Street, Fifth Floor, Boston, MA 02110-1301 USA.
 */

/* CRT scaling with a live Original/Zoom choice. Retain the source
 * frame so changing geometry while paused never decodes or seeks another frame.
 * Only the negotiated MPEG-2 transcode's planar 4:2:0 formats are accepted. */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <math.h>
#include "config.h"
#include "img_format.h"
#include "mp_image.h"
#include "vf.h"
#include "fmt-conversion.h"
#include "libswscale/swscale.h"
#include "libvo/fastmemcpy.h"

struct vf_priv_s {
    int width, height, mode, have_frame;
    double dar, pts, endpts;
    mp_image_t *source, *scaled;
    struct SwsContext *scaler[2], *converter[2];
};

/* Source offsets and output dimensions stay even for 4:2:0 chroma. */
static int render(vf_instance_t *vf)
{
    struct vf_priv_s *p = vf->priv;
    mp_image_t *src = p->source;
    int cw = src->w, ch = src->h, left = 0, top = 0, w = p->width;
    double par = (double)p->width * 3 / (p->height * 4);
    int h = ((int)(w / (p->dar * par) + 0.5)) & ~1;
    int zoom = p->mode;
    if (zoom) {
        if (p->dar > 4.0 / 3 + 0.01) {
            cw = ((int)(src->w * (4.0 / 3) / p->dar + 0.5)) & ~1;
        } else if (p->dar < 4.0 / 3 - 0.01) {
            ch = ((int)(src->h * p->dar / (4.0 / 3) + 0.5)) & ~1;
        } else {
            /* A fixed 4/3 enlargement removes common baked-in letterboxing. */
            cw = ((int)(src->w * 0.75 + 0.5)) & ~1;
            ch = ((int)(src->h * 0.75 + 0.5)) & ~1;
        }
        if (cw < 2) cw = 2;
        if (ch < 2) ch = 2;
        if (cw > src->w) cw = src->w & ~1;
        if (ch > src->h) ch = src->h & ~1;
        left = ((src->w - cw) / 2) & ~1;
        top = ((src->h - ch) / 2) & ~1;
        h = p->height;
    } else if (h > p->height) {
        h = p->height;
        w = ((int)(h * p->dar * par + 0.5)) & ~1;
    }
    if (w < 2) w = 2;
    if (h < 2) h = 2;
    /* Full-height resizing directly to RGB uses the scalar color converter.
     * Resize in YUV first so the second pass can use ARM's unscaled NEON
     * conversion. Keep the existing path for progressive output and sources
     * that already fit. NEON requires a width divisible by 16. */
    int split = p->height >= 480 && (cw != w || ch != h) && !(w & 15);
    struct SwsContext *ctx = sws_getCachedContext(p->scaler[zoom],
        cw, ch, imgfmt2pixfmt(src->imgfmt), w, h,
        imgfmt2pixfmt(split ? IMGFMT_YV12 : IMGFMT_BGR32), SWS_FAST_BILINEAR, NULL, NULL, NULL);
    p->scaler[zoom] = ctx;
    if (!ctx) return 0;
    mp_image_t *out = vf_get_image(vf->next, IMGFMT_BGR32, MP_IMGTYPE_TEMP,
        MP_IMGFLAG_ACCEPT_STRIDE | MP_IMGFLAG_PREFER_ALIGNED_STRIDE,
        p->width, p->height);
    if (!out) return 0;
    /* The output adapter lends its clean RAM buffer. Black bars and the
     * scaled picture are complete before it composites the Go overlay. */
    if (w != p->width || h != p->height)
        for (int y = 0; y < p->height; y++)
            memset(out->planes[0] + y * out->stride[0], 0, p->width * 4);
    const uint8_t *planes[4] = {
        src->planes[0] + top * src->stride[0] + left,
        src->planes[1] + top / 2 * src->stride[1] + left / 2,
        src->planes[2] + top / 2 * src->stride[2] + left / 2,
        NULL
    };
    uint8_t *dest[4] = {out->planes[0] + (p->height - h) / 2 * out->stride[0]
        + (p->width - w) / 2 * 4, NULL, NULL, NULL};
    if (split) {
        struct SwsContext *rgb = sws_getCachedContext(p->converter[zoom],
            w, h, imgfmt2pixfmt(IMGFMT_YV12), w, h,
            imgfmt2pixfmt(IMGFMT_BGR32), SWS_FAST_BILINEAR, NULL, NULL, NULL);
        p->converter[zoom] = rgb;
        if (!rgb || sws_scale(ctx, planes, src->stride, 0, ch,
                            p->scaled->planes, p->scaled->stride) != h)
            return 0;
        const uint8_t *scaled[] = {p->scaled->planes[0], p->scaled->planes[1],
                                  p->scaled->planes[2], NULL};
        if (sws_scale(rgb, scaled, p->scaled->stride, 0, h, dest, out->stride) != h)
            return 0;
    } else if (sws_scale(ctx, planes, src->stride, 0, ch, dest, out->stride) != h) {
        return 0;
    }
    return vf_next_put_image(vf, out, p->pts, p->endpts);
}

static int config(vf_instance_t *vf, int w, int h, int dw, int dh,
                  unsigned int flags, unsigned int fmt)
{
    struct vf_priv_s *p = vf->priv;
    if (w < 2 || h < 2 || w > 4096 || h > 4096 || (w & 1) || (h & 1)) return 0;
    free_mp_image(p->source);
    p->source = NULL;
    free_mp_image(p->scaled);
    p->scaled = p->height >= 480 ? alloc_mpi(p->width, p->height, IMGFMT_YV12) : NULL;
    if (p->height >= 480 && (!p->scaled || !p->scaled->planes[0])) return 0;
    /* Keep every luma/chroma row aligned for ARM's scaler and copy paths. */
    p->source = alloc_mpi((w + 63) & ~63, h, fmt);
    if (p->source) p->source->w = w;
    p->have_frame = 0;
    if (!p->source || !p->source->planes[0]) return 0;
    return vf_next_config(vf, p->width, p->height, p->width, p->height, flags, IMGFMT_BGR32);
}

static int put_image(vf_instance_t *vf, mp_image_t *mpi, double pts, double endpts)
{
    struct vf_priv_s *p = vf->priv;
    /* Decoder strides can include padding wider than the visible image. */
    for (int plane = 0; plane < 3; plane++) {
        int shift = plane != 0;
        memcpy_pic(p->source->planes[plane], mpi->planes[plane],
            mpi->w >> shift, mpi->h >> shift,
            p->source->stride[plane], mpi->stride[plane]);
    }
    p->pts = pts;
    p->endpts = endpts;
    p->have_frame = 1;
    return render(vf);
}

static int control(vf_instance_t *vf, int request, void *data)
{
    struct vf_priv_s *p = vf->priv;
    if (request == VFCTRL_MISTERVISION_PICTURE) {
        int mode = *(int *)data;
        if (mode < 0 || mode > 1 || !p->have_frame) return CONTROL_FALSE;
        int old = p->mode;
        p->mode = mode;
        if (!render(vf)) {
            p->mode = old;
            return CONTROL_FALSE;
        }
        /* Repaint the cached frame without changing decoder or audio clocks. */
        vf_extra_flip(vf);
        return CONTROL_TRUE;
    }
    return vf_next_control(vf, request, data);
}

static int query_format(vf_instance_t *vf, unsigned int fmt)
{
    if (fmt != IMGFMT_YV12 && fmt != IMGFMT_I420 && fmt != IMGFMT_IYUV) return 0;
    return vf_next_query_format(vf, IMGFMT_BGR32) & ~VFCAP_CSP_SUPPORTED_BY_HW;
}

static void uninit(vf_instance_t *vf)
{
    struct vf_priv_s *p = vf->priv;
    free_mp_image(p->source);
    free_mp_image(p->scaled);
    sws_freeContext(p->converter[0]);
    sws_freeContext(p->converter[1]);
    sws_freeContext(p->scaler[0]);
    sws_freeContext(p->scaler[1]);
    free(p);
}

static int vf_open(vf_instance_t *vf, char *args)
{
    struct vf_priv_s *p = calloc(1, sizeof(*p));
    if (!p) return 0;
    if (!args || sscanf(args, "%d:%d:%lf:%d", &p->width, &p->height, &p->dar, &p->mode) != 4 ||
        p->width != 640 || (p->height != 240 && p->height != 288 && p->height != 480 && p->height != 576) ||
        !isfinite(p->dar) || p->dar < 0.1 || p->dar > 10 || p->mode < 0 || p->mode > 1) {
        free(p);
        return 0;
    }
    vf->priv = p;
    vf->config = config;
    vf->put_image = put_image;
    vf->control = control;
    vf->query_format = query_format;
    vf->uninit = uninit;
    return 1;
}

const vf_info_t vf_info_mistervision = {
    "MiSTerVision live CRT picture fit", "mistervision", "", "", vf_open, NULL
};
