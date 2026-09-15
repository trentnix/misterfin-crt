// MiSTer interlaced scanout control. Main must be stopped before these calls.
// Register protocol follows the C client's framebuffer adapter (CC BY-NC 4.0).
// Copyright © 2026 Pudding Studio. Modifications copyright © 2026 trentnix.
#include <time.h>

struct interlaced_scanout {
    void *map;
    volatile uint32_t *gpo, *gpi;
    uint32_t address;
};

static int scanout_open(struct interlaced_scanout *s, int fb)
{
    struct fb_fix_screeninfo info;
    if (ioctl(fb, FBIOGET_FSCREENINFO, &info) < 0) return errno;
    if (info.smem_start > UINT32_MAX) return ENOTSUP;
    int fd = open("/dev/mem", O_RDWR | O_SYNC | O_CLOEXEC);
    if (fd < 0) return errno;
    s->map = mmap(NULL, 0x1000000, PROT_READ | PROT_WRITE, MAP_SHARED, fd, 0xFF000000);
    close(fd);
    if (s->map == MAP_FAILED) { s->map = NULL; return errno; }
    s->gpo = (volatile uint32_t *)((uint8_t *)s->map + 0x706010);
    s->gpi = (volatile uint32_t *)((uint8_t *)s->map + 0x706014);
    s->address = info.smem_start;
    return 0;
}

static int scanout_ack(struct interlaced_scanout *s, int high)
{
    struct timespec start, now;
    clock_gettime(CLOCK_MONOTONIC, &start);
    for (unsigned count = 0; ; ++count) {
        if (!!(*s->gpi & (1u << 17)) == high) return 0;
        if (!(count & 1023)) {
            clock_gettime(CLOCK_MONOTONIC, &now);
            if ((now.tv_sec - start.tv_sec) * 1000000000LL + now.tv_nsec - start.tv_nsec > 60000000)
                return ETIMEDOUT;
        }
    }
}

static int scanout_word(struct interlaced_scanout *s, uint32_t base, uint16_t word)
{
    uint32_t value = (base & ~(0xFFFFu | (1u << 17))) | word;
    *s->gpo = value;
    *s->gpo = value | (1u << 17);
    int error = scanout_ack(s, 1);
    *s->gpo = value;
    if (!error) error = scanout_ack(s, 0);
    return error;
}

// scanout_select restores the UI page after normal or forced decoder shutdown.
// Geometry remains exactly as Main configured it. No polling thread runs.
static int scanout_select(struct interlaced_scanout *s)
{
    uint32_t base = *s->gpo | 0x80000000u | (1u << 20);
    *s->gpo = base;
    int error = 0;
    const uint16_t words[] = {0x2F, 0x8016, s->address & 0xFFFF, s->address >> 16};
    for (unsigned i = 0; !error && i < sizeof(words)/sizeof(words[0]); i++)
        error = scanout_word(s, base, words[i]);
    *s->gpo = base & ~(1u << 20);
    return error;
}
