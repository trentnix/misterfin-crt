/* Decode ATSC's EIA-608 compatibility captions from already decoded frames.
 * This adapter is part of the private MPlayer build, under MPlayer's GPL-2.0-or-later
 * license. FFmpeg owns caption decoding. Go owns selection and rendering.
 */
#ifndef MISTERFIN_CAPTIONS_H
#define MISTERFIN_CAPTIONS_H
#include <stdio.h>
#include <string.h>
#include "libavcodec/avcodec.h"
#include "libavutil/opt.h"
#include "libavutil/time.h"

#define MF_CAPTION_TEXT_LIMIT 2000

struct mf_captions {
    AVCodecContext *decoder;
    int attempted;
    char last[MF_CAPTION_TEXT_LIMIT];
};

/* Hex keeps line breaks, UTF-8, and media text out of the status protocol. */
static void mf_caption_emit(const char *text) {
    static const char hex[] = "0123456789abcdef";
    char line[sizeof("ANS_CAPTION_ASS=") + 2 * MF_CAPTION_TEXT_LIMIT + 1];
    size_t n = strlen("ANS_CAPTION_ASS=");
    memcpy(line, "ANS_CAPTION_ASS=", n);
    for (size_t i = 0; text[i] && i < MF_CAPTION_TEXT_LIMIT - 1; i++) {
        unsigned char c = text[i];
        line[n++] = hex[c >> 4];
        line[n++] = hex[c & 15];
    }
    line[n++] = '\n';
    fwrite(line, 1, n, stdout);
    fflush(stdout);
}

/* Called once per decoded frame, after FFmpeg has reordered video and its A53
 * side data. Only text changes cross the pipe. No second video decode is needed.
 * CC1 is the first field's primary EIA-608 service, not a full CEA-708 decoder.
 */
static void mf_caption_frame(struct mf_captions *state, const AVFrame *frame) {
    AVFrameSideData *side = av_frame_get_side_data(frame, AV_FRAME_DATA_A53_CC);
    if (!side || side->size < 3) return;
    if (!state->attempted) {
        state->attempted = 1;
        const AVCodec *codec = avcodec_find_decoder(AV_CODEC_ID_EIA_608);
        if (!codec) return;
        state->decoder = avcodec_alloc_context3(codec);
        if (!state->decoder) return;
        state->decoder->pkt_timebase = (AVRational){1, AV_TIME_BASE};
        av_opt_set_int(state->decoder->priv_data, "real_time", 1, 0);
        av_opt_set_int(state->decoder->priv_data, "data_field", 0, 0);
        if (avcodec_open2(state->decoder, codec, NULL) < 0) {
            avcodec_free_context(&state->decoder);
            return;
        }
        mf_caption_emit(""); /* Advertise decoder availability even before text. */
    }
    if (!state->decoder) return;
    AVPacket packet = {0};
    packet.data = side->data;
    packet.size = side->size / 3 * 3;
    /* Realtime mode uses this clock to coalesce roll-up updates, not to schedule
     * video. Caption packets are already associated with the current frame. */
    packet.pts = av_gettime_relative();
    AVSubtitle sub = {0};
    int got = 0;
    int result = avcodec_decode_subtitle2(state->decoder, &sub, &got, &packet);
    if (result >= 0 && got && sub.num_rects) {
        /* Realtime rectangles are successive screen snapshots. Use the last. */
        const char *text = sub.rects[sub.num_rects - 1]->ass;
        if (text) {
            for (int i = 0; i < 8 && text; i++) {
                text = strchr(text, ',');
                if (text) text++;
            }
            if (text) {
                char next[sizeof(state->last)];
                snprintf(next, sizeof(next), "%s", text);
                if (strcmp(next, state->last)) {
                    memcpy(state->last, next, strlen(next) + 1);
                    mf_caption_emit(next);
                }
            }
        }
    }
    avsubtitle_free(&sub);
}
#endif
