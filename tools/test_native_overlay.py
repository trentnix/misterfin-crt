"""Exercise the actual Go MPlayer adapter against memory-backed scanout pages."""
import pathlib
import shutil
import subprocess
import tempfile
import unittest

ROOT = pathlib.Path(__file__).resolve().parents[1]


class NativeOverlayTest(unittest.TestCase):
    def test_display_owns_console_mode_until_close(self):
        program = r'''
#include <assert.h>
#include <stdarg.h>
#include <stdint.h>
#include <string.h>
#include <fcntl.h>
#include <linux/fb.h>
#include <linux/kd.h>
#include <sys/ioctl.h>
#include <sys/mman.h>
#include <unistd.h>
static int mode = KD_TEXT, switches, fallback;
static unsigned char pixels[32];
static int mock_open(const char *path, int flags, ...) {
    if (!strcmp(path, "/dev/fb0")) return 10;
    if (!strcmp(path, "/dev/tty")) return -1;
    if (!strcmp(path, "/dev/tty0")) { fallback++; return 11; }
    return -1;
}
static int clears;
static ssize_t mock_write(int fd, const void *data, size_t size) {
    assert(fd == 11);
    assert(strstr((const char *)data, "\033[2J"));
    clears++;
    return size;
}
static int mock_close(int fd) { return 0; }
static void *mock_mmap(void *p, size_t n, int prot, int flags, int fd, off_t offset) { return pixels; }
static int mock_munmap(void *p, size_t n) { return 0; }
static int mock_ioctl(int fd, unsigned long request, ...) {
    va_list args;
    va_start(args, request);
    if (request == KDSETMODE) { mode = va_arg(args, int); switches++; }
    else if (request == KDGETMODE) { *va_arg(args, int *) = mode; }
    else if (request == FBIOGET_VSCREENINFO) {
        struct fb_var_screeninfo *v = va_arg(args, void *);
        memset(v, 0, sizeof(*v));
        v->xres = 4; v->yres = 2; v->bits_per_pixel = 32;
        v->red.offset = 16; v->green.offset = 8;
        v->red.length = v->green.length = v->blue.length = 8;
    } else if (request == FBIOGET_FSCREENINFO) {
        struct fb_fix_screeninfo *f = va_arg(args, void *);
        memset(f, 0, sizeof(*f));
        f->type = FB_TYPE_PACKED_PIXELS; f->visual = FB_VISUAL_TRUECOLOR;
        f->line_length = 16; f->smem_len = 32;
    }
    va_end(args);
    return 0;
}
#define write mock_write
#define open mock_open
#define close mock_close
#define mmap mock_mmap
#define munmap mock_munmap
#define ioctl mock_ioctl
#include "adapter_linux.c"
int main(void) {
    mf_display *d;
    memset(pixels, 0x55, sizeof(pixels));
    assert(mf_open(&d, "/dev/fb0", 0, 0) == 0);
    for (int i = 0; i < sizeof(pixels); i++) assert(pixels[i] == 0);
    assert(clears == 1);
    memset(pixels, 0x99, sizeof(pixels));
    assert(mode == KD_GRAPHICS && switches == 1 && fallback == 1);
    assert(mf_close(d) == 0 && mode == KD_TEXT && switches == 2);
    for (int i = 0; i < sizeof(pixels); i++) assert(pixels[i] == 0);
    assert(clears == 2);
    mode = KD_GRAPHICS;
    assert(mf_open(&d, "/dev/fb0", 0, 0) == 0);
    assert(mf_close(d) == 0 && mode == KD_GRAPHICS);
    int before = switches;
    assert(mf_open(&d, "", 4, 2) == 0);
    assert(mf_close(d) == 0 && switches == before);
    return 0;
}
'''
        with tempfile.TemporaryDirectory() as directory:
            source = pathlib.Path(directory) / 'console.c'
            binary = pathlib.Path(directory) / 'console'
            source.write_text(program)
            subprocess.run(['cc', '-D_GNU_SOURCE', '-fsanitize=undefined', '-I', str(ROOT / 'internal/platform'), str(source), '-o', str(binary)], check=True)
            subprocess.run([str(binary)], check=True)

    def test_composes_before_scanout_and_retains_clean_paused_frame(self):
        with tempfile.TemporaryDirectory() as directory:
            work = pathlib.Path(directory)
            source = work / 'vo_fbdev.c'
            shutil.copy(ROOT / 'docker/vo_fbdev.c', source)
            subprocess.run(['patch', str(source), str(ROOT / 'docker/vo_fbdev_go.patch')], check=True, capture_output=True)
            text = source.read_text()
            # Include the whole function, including its internal comments.
            start = text.index('static void overlay_frame')
            end = text.index('\n}\n', start) + 3
            adapter = text[text.index('#define OVERLAY_FILE'):end]
            start = text.index('static int draw_slice(')
            end = text.index('\n}\n', start) + 3
            draw = text[start:end]
            program = r'''
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <stdio.h>
#include <assert.h>
#include <fcntl.h>
#include <unistd.h>
#include <sys/stat.h>
#include <sys/file.h>
#include <errno.h>
#include <sys/ioctl.h>
#include <linux/fb.h>
#define VO_FALSE 0
static int in_width = 4, in_height = 2, fb_line_len = 20, fb_pixel_size = 4;
static int pf_active = 1, fb_dev_fd = -1;
static int waits;
static int test_ioctl(int fd, unsigned long request, void *arg) { waits++; return 0; }
#define ioctl test_ioctl
static uint8_t pages[2][40];
static uint8_t *center = pages[0];
static int ban_clip_top(void) { return in_height; }
static void memcpy_pic2(uint8_t *d, uint8_t *s, int w, int h, int ds, int ss, int unused) {
    for (int y = 0; y < h; y++) memcpy(d + y * ds, s + y * ss, w);
}
''' + adapter.replace('"/tmp/misterfin_go_overlay"', '"overlay"') + draw.replace('"/tmp/misterdvd_vsync"', '"vsync"') + r'''
static void publish(void) {
    uint8_t header[40] = {'M','F','G','O','O','V','1',0};
    header[8] = 4; header[12] = 2;
    header[16] = 1; header[20] = 0;
    header[24] = 1; header[28] = 1; header[32] = 1;
    uint8_t pixel[4] = {200,200,200,128};
    FILE *f = fopen("overlay", "wb"); assert(f);
    assert(fwrite(header, 1, 40, f) == 40);
    assert(fwrite(pixel, 1, 4, f) == 4); fclose(f);
}
int main(void) {
    publish();
    int probe = open(OUTPUT_LOCK_FILE, O_CREAT | O_RDWR, 0600);
    assert(probe >= 0);
    memset(pages, 77, sizeof(pages));
    uint8_t video[40]; uint8_t *src[] = {video}; int stride[] = {20};
    for (int frame = 0; frame < 60; frame++) {
        center = pages[frame % 2];
        uint8_t before[40]; memcpy(before, center, 40);
        memset(video, frame, sizeof(video));
        draw_slice(src, stride, 4, 2, 0, 0);
        /* Decoder writes must not erase any part of the displayed menu. */
        assert(memcmp(before, center, 40) == 0);
        if (frame == 0) {
            /* Preparing a decoded frame still leaves loading output to Go. */
            assert(flock(probe, LOCK_EX | LOCK_NB) == 0);
            assert(flock(probe, LOCK_UN) == 0);
        }
        overlay_frame();
        assert(flock(probe, LOCK_EX | LOCK_NB) == -1);
        assert(errno == EWOULDBLOCK); /* video now owns scanout */
        int blended = (200 * 128 + frame * 127 + 127) / 255;
        assert(center[4] == blended && center[0] == frame);
        assert(center[16] == 77); /* stride padding is not image data */
        overlay_frame(); /* paused refresh must not compound alpha */
        assert(center[4] == blended);
        assert(go_video[4] == frame);
    }
    unlink("overlay");
    overlay_frame(); /* hiding while paused restores clean video */
    assert(center[4] == 59);
    /* Synchronization belongs before MPlayer's presentation deadline. */
    FILE *flag = fopen("vsync", "w"); assert(flag); fclose(flag);
    pf_active = 0;
    draw_slice(src, stride, 4, 2, 0, 0);
    assert(waits == 1);
    overlay_frame();
    assert(waits == 1); /* presentation must not wait a second time */
    unlink("vsync");
    overlay_discard(); free(go_video); free(go_row);
    overlay_release_output();
    assert(flock(probe, LOCK_EX | LOCK_NB) == 0);
    close(probe);
    return 0;
}
'''
            harness = work / 'test.c'
            harness.write_text(program)
            subprocess.run(['cc', '-std=c99', '-fsanitize=undefined', '-g', str(harness), '-o', str(work / 'test')], check=True)
            result = subprocess.run([str(work / 'test')], cwd=work, check=True, capture_output=True, text=True)
            self.assertEqual(result.stdout, 'ANS_VIDEO_STARTED=true\n')


if __name__ == '__main__':
    unittest.main()
