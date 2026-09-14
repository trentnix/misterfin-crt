//go:build linux && cgo

#include "adapter.h"
#include <dlfcn.h>
#include <errno.h>
#include <stdlib.h>

/* Resolve ALSA on the target, as the C client does. Cross-compilation needs
 * neither ALSA headers nor an ARM development library. Each stream owns its
 * symbols and PCM handle. Go serializes every call for that stream. */
struct mf_sound {
    void *library, *pcm;
    int (*open)(void **, const char *, int, int);
    int (*params)(void *, int, int, unsigned, unsigned, int, unsigned);
    int (*prepare)(void *);
    int (*drop)(void *);
    int (*close)(void *);
    long (*write)(void *, const void *, unsigned long);
};

/* Open nonblocking stereo S16_LE at 48 kHz, matching MiSTer's audio sink.
 * A busy or missing device simply disables this burst of feedback. */
mf_sound *mf_sound_open(void) {
    mf_sound *s = calloc(1, sizeof(*s));
    if (!s) return NULL;
    s->library = dlopen("libasound.so.2", RTLD_NOW | RTLD_LOCAL);
    if (!s->library) goto fail;
#define LOAD(field, name) do { *(void **)(&s->field) = dlsym(s->library, name); if (!s->field) goto fail; } while (0)
    LOAD(open, "snd_pcm_open");
    LOAD(params, "snd_pcm_set_params");
    LOAD(prepare, "snd_pcm_prepare");
    LOAD(drop, "snd_pcm_drop");
    LOAD(close, "snd_pcm_close");
    LOAD(write, "snd_pcm_writei");
#undef LOAD
    if (s->open(&s->pcm, "default", 0, 1) < 0) goto fail;
    /* Format 2 = S16_LE. Access 3 = RW_INTERLEAVED. No resampling needed. */
    if (s->params(s->pcm, 2, 3, 2, 48000, 0, 20000) < 0) goto fail;
    return s;
fail:
    mf_sound_close(s);
    return NULL;
}

/* Never wait for device capacity. Recover a simple underrun without ALSA's
 * potentially blocking suspend-recovery loop. Go retries on its next tick. */
long mf_sound_write(mf_sound *s, const int16_t *samples, unsigned long frames) {
    long n = s->write(s->pcm, samples, frames);
    if (n == -EAGAIN) return 0;
    if (n == -EPIPE && s->prepare(s->pcm) >= 0) return 0;
    return n;
}

/* Drop instead of drain so MPlayer can acquire audio immediately. */
void mf_sound_close(mf_sound *s) {
    if (!s) return;
    if (s->pcm) {
        if (s->drop) s->drop(s->pcm);
        if (s->close) s->close(s->pcm);
    }
    if (s->library) dlclose(s->library);
    free(s);
}
