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
            subprocess.run(['patch', str(source), str(ROOT / 'docker/vo_fbdev_interlaced.patch')], check=True, capture_output=True)
            text = source.read_text()
            # Include the whole function, including its internal comments.
            start = text.index('static void overlay_frame')
            end = text.index('\n}\n', start) + 3
            adapter = text[text.index('#define OVERLAY_FILE'):end].replace('"/sys/module/MiSTer_fb/parameters/frame_count"', '"field_count"')
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
static uint8_t *frame_buffer = pages[0], *pf_page[2] = {pages[0],pages[1]};
static int fb_yres=480, pf_back=1, pf_inits, pf_field_fd=-1;
static size_t pf_center_off;
static int pf_init(void) { pf_inits++; return 1; }
#define mp_msg(...) ((void)0)
static int ban_clip_top(void) { return in_height; }
static void memcpy_pic2(uint8_t *d, uint8_t *s, int w, int h, int ds, int ss, int unused) {
    for (int y = 0; y < h; y++) memcpy(d + y * ds, s + y * ss, w);
}
''' + adapter.replace('"/tmp/mistervision_overlay"', '"overlay"') + draw.replace('"/tmp/misterdvd_vsync"', '"vsync"') + r'''
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
int main(int argc, char **argv) {
    if (argc > 1) setenv("MISTERVISION_INTERLACED","1",1);
    FILE *counter = fopen("field_count", "w"); assert(counter); fclose(counter);
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
    assert(waits == (argc > 1 ? 0 : 1));
    overlay_frame();
    assert(waits == (argc > 1 ? 0 : 1)); /* presentation must not wait a second time */
    unlink("vsync");
    overlay_discard(); free(go_video); free(go_row);
    assert(pf_inits == (argc > 1 ? 1 : 0));
    overlay_release_output();
    assert(flock(probe, LOCK_EX | LOCK_NB) == 0);
    close(probe);
    if (pf_field_fd >= 0) close(pf_field_fd);
    return 0;
}
'''
            harness = work / 'test.c'
            harness.write_text(program)
            subprocess.run(['cc', '-D_GNU_SOURCE', '-std=c99', '-fsanitize=undefined', '-g', str(harness), '-o', str(work / 'test')], check=True)
            result = subprocess.run([str(work / 'test')], cwd=work, check=True, capture_output=True, text=True)
            self.assertEqual(result.stdout, '')
            result = subprocess.run([str(work / 'test'), 'interlaced'], cwd=work, check=True, capture_output=True, text=True)
            self.assertEqual(result.stdout, '')

    def patched_driver(self, work):
        source = work / 'vo_fbdev.c'
        shutil.copy(ROOT / 'docker/vo_fbdev.c', source)
        for patch in ('vo_fbdev_go.patch', 'vo_fbdev_interlaced.patch'):
            subprocess.run(['patch', str(source), str(ROOT / 'docker' / patch)], check=True, capture_output=True)
        return source.read_text()

    def test_prepares_before_deadline_and_protects_scanout(self):
        with tempfile.TemporaryDirectory() as directory:
            work = pathlib.Path(directory)
            text = self.patched_driver(work)
            def function(name):
                start = text.index('static void ' + name + '(void)')
                end = text.index('\n}\n', start) + 3
                return text[start:end]
            start = text.index('#define OVERLAY_FILE')
            end = text.index('\n}\n', text.index('static void overlay_frame')) + 3
            adapter = text[start:end].replace('"/tmp/mistervision_overlay"', '"overlay"').replace('"/sys/module/MiSTer_fb/parameters/frame_count"', '"field_count"')
            program = r'''
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <stdio.h>
#include <assert.h>
#include <fcntl.h>
#include <unistd.h>
#include <errno.h>
#include <sys/stat.h>
#include <sys/file.h>
#include <sys/ioctl.h>
#include <linux/fb.h>
static int in_width=4, in_height=2, fb_line_len=16, fb_pixel_size=4;
static int fb_dev_fd=-1, fb_yres=480, fb_page, vo_doublebuffering;
static struct fb_var_screeninfo fb_vinfo;
static unsigned char pages[2][32];
static unsigned char *center=pages[0], *frame_buffer=pages[0], *pf_page[2]={pages[0],pages[1]};
static int pf_active, pf_pending, pf_prepared, pf_back=1;
static size_t pf_center_off;
static int displayed, submitted, waits, flips, writes;
static int pf_init(void) {return 1;}
static unsigned field;
static void pf_flip_to(int page) {assert(page != displayed); submitted=page; flips++;}
static unsigned pf_field(void) {return field;}
static unsigned pf_submitted_field;
static int pf_field_fd=-1;
static void pf_release_page(void) {if(pf_pending) {displayed=submitted;field++;waits++;pf_pending=0;}}
static int mock_ioctl(int fd, unsigned long request, void *arg) {
    assert(request == FBIO_WAITFORVSYNC);
    displayed=submitted; waits++; return 0;
}
#define ioctl mock_ioctl
#define mp_msg(...) ((void)0)
static void *checked_copy(void *dst, const void *src, size_t n) {
    if ((uintptr_t)dst >= (uintptr_t)pages && (uintptr_t)dst < (uintptr_t)(pages+2)) {
        if (pf_active) assert((uintptr_t)dst < (uintptr_t)pages[displayed] || (uintptr_t)dst >= (uintptr_t)pages[displayed]+32);
        writes++;
    }
    return memcpy(dst,src,n);
}
#define memcpy checked_copy
static void ban_frame(void) {}
#define draw_alpha 0
static void vo_draw_text(int w, int h, int unused) {}
''' + adapter + function('prepare_frame') + function('flip_page') + function('draw_osd') + r'''
int main(int argc, char **argv) {
    FILE *counter = fopen("field_count", "w"); assert(counter); fclose(counter);
    int interlaced = argc > 1;
    if(interlaced) setenv("MISTERVISION_INTERLACED","1",1);
    assert(overlay_prepare());
    for(int i=1;i<=100;i++) {
        memset(go_video,i,32);
        int oldwrites=writes, oldwaits=waits;
        draw_osd();
        assert(writes-oldwrites == (interlaced ? 2 : 0));
        if(i==1) assert(!go_video_announced);
        oldwrites=writes; oldwaits=waits;
        flip_page();
        assert(go_video_announced);
        assert(waits==oldwaits);
        assert(writes-oldwrites == (interlaced ? 0 : 2));
        for(int j=0;j<32;j++) assert(pages[interlaced?submitted:0][j] == i);
        /* A paused refresh can arrive before the pending flip is latched. */
        draw_osd();
        flip_page();
        for(int j=0;j<32;j++) assert(pages[interlaced?submitted:0][j] == i);
    }
    assert(waits == (interlaced ? flips-1 : 0));
    overlay_release_output();
    free(go_video); free(go_row);
    if (pf_field_fd >= 0) close(pf_field_fd);
}
'''
            (work / 'test.c').write_text(program)
            subprocess.run(['cc', '-fsanitize=undefined', str(work / 'test.c'), '-o', str(work / 'test')], check=True)
            for args in ([], ['interlaced']):
                result = subprocess.run([str(work / 'test'), *args], cwd=work, check=True, capture_output=True, text=True)
                self.assertEqual(result.stdout, 'ANS_VIDEO_STARTED=true\n')

    def test_page_release_handles_elapsed_fields_wrap_and_failures(self):
        with tempfile.TemporaryDirectory() as directory:
            work = pathlib.Path(directory)
            text = self.patched_driver(work)
            start = text.index('static int pf_pending, pf_prepared;')
            end = text.index('static int      pf_active;',start)
            body = text[start:end]
            code = r'''
#include <stdint.h>
#include <unistd.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <errno.h>
#include <assert.h>
#include <sys/wait.h>
static int64_t now;
static uint32_t field=UINT32_MAX;
static int reads, sleeps, freeze, bad, interrupt;
#define mp_msg(...) ((void)0)
static int64_t av_gettime_relative(void) {return now;}
static void av_usleep(unsigned us) {now+=us;sleeps++;if(!freeze)field++;}
static ssize_t mock_pread(int fd, void *p, size_t n, off_t off) {
    reads++;
    if(interrupt) {interrupt=0;errno=EINTR;return -1;}
    if(bad==1) {errno=EIO;return -1;}
    if(bad==2) return snprintf(p,n,"bad\n");
    return snprintf(p,n,"%u\n",field);
}
#define pread mock_pread
''' + body + r'''
int main(void) {
    pf_release_page();assert(!reads&&!sleeps);
    pf_submitted_field=field;pf_pending=1;
    interrupt=1;
    pf_release_page();assert(field==0&&!pf_pending&&sleeps==1);
    pf_submitted_field=0;pf_pending=1;field=1;
    pf_release_page();assert(!pf_pending&&sleeps==1);
    for(int mode=0;mode<3;mode++) {
        pid_t child=fork();assert(child>=0);
        if(!child) {
            freeze=mode==0;bad=mode;pf_submitted_field=field;pf_pending=1;
            pf_release_page();_exit(0);
        }
        int status;assert(waitpid(child,&status,0)==child);
        assert(WIFEXITED(status)&&WEXITSTATUS(status)==1);
    }
}
'''
            (work / 'test.c').write_text(code)
            subprocess.run(['cc', '-fsanitize=undefined', str(work / 'test.c'), '-o', str(work / 'test')], check=True)
            subprocess.run([str(work / 'test')], cwd=work, check=True)


if __name__ == '__main__':
    unittest.main()
