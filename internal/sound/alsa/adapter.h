#ifndef MISTERVISION_SOUND_ADAPTER_H
#define MISTERVISION_SOUND_ADAPTER_H
#include <stdint.h>
typedef struct mf_sound mf_sound;
mf_sound *mf_sound_open(void);
long mf_sound_write(mf_sound *s, const int16_t *samples, unsigned long frames);
void mf_sound_close(mf_sound *s);
#endif
