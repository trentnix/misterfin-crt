"""Exercise private MPlayer timing and paused-overlay behavior."""
import pathlib
import shutil
import subprocess
import tempfile
import unittest

ROOT = pathlib.Path(__file__).resolve().parents[1]


class MPlayerTimingTest(unittest.TestCase):
    def test_dropped_frames_advance_the_timeline_only_once(self):
        with tempfile.TemporaryDirectory() as directory:
            work = pathlib.Path(directory)
            source = work / "mplayer.c"
            shutil.copy(ROOT / "tools/testdata/mplayer-video-timing.c", source)
            subprocess.run(["patch", str(source), str(ROOT / "docker/mplayer_go.patch")], check=True, capture_output=True)
            program = r'''
#include <assert.h>
#include <math.h>
#define MP_NOPTS_VALUE (-1e20)
#define VFCTRL_GET_PTS 1
#define VFCTRL_GET_ENDPTS 2
#define MSGT_CPLAYER 0
#define MSGL_ERR 0
#define MSGL_V 0
#define MSGTR_PtsAfterFiltersMissing "missing"
#define mp_msg(...) ((void)0)
typedef struct vf_instance {
    void (*control)(void *, int, double *);
} vf_instance_t;
typedef struct {
    double pts, last_pts, endpts, frametime;
    void *vfilter;
} sh_video_t;
static struct {
    sh_video_t *sh_video;
    void *d_video;
    int startup_decode_retry;
} context, *mpctx = &context;
static int result, filter_reads;
static double filter_pts, elapsed;
static int generate_video_frame(sh_video_t *v, void *d) { return result; }
static void advance_timer(double dt) { elapsed += dt; }
static void filter_control(void *vf, int request, double *pts) {
    filter_reads++;
    *pts = request == VFCTRL_GET_PTS ? filter_pts : MP_NOPTS_VALUE;
}
#include "mplayer.c"
static void close_to(double actual, double expected) {
    assert(fabs(actual - expected) < 1e-8);
}
int main(void) {
    vf_instance_t vf = {filter_control};
    sh_video_t video = {.frametime=1001.0/30000, .vfilter=&vf};
    mpctx->sh_video = &video;
    for (int drops = 0; drops <= 8; drops++) {
        video.pts = video.last_pts = filter_pts = 10;
        elapsed = 0;
        /* Repeated drops must neither query stale filter PTS nor make the
         * next displayed frame count already-consumed intervals again. */
        for (int i = 0; i < drops; i++) {
            result = -1;
            filter_reads = 0;
            int blit = 1;
            close_to(update_video(&blit), video.frametime);
            assert(!blit && !filter_reads);
            close_to(video.pts, 10 + (i+1)*video.frametime);
        }
        result = 1;
        filter_pts = 10 + (drops+1)*video.frametime;
        int blit = 0;
        close_to(update_video(&blit), video.frametime);
        assert(blit);
        close_to(elapsed, filter_pts - 10);
    }
    /* Alternating drops used to create a persistent slow-playback cycle. */
    video.pts = video.last_pts = filter_pts = 10;
    elapsed = 0;
    for (int i = 1; i <= 120; i++) {
        result = i % 2 ? -1 : 1;
        if (result > 0) filter_pts = 10 + i*video.frametime;
        int blit = 0;
        close_to(update_video(&blit), video.frametime);
        assert(blit == (result > 0));
    }
    close_to(elapsed, 120*video.frametime);
    /* A real timestamp gap still determines timing for the next image. */
    video.pts = video.last_pts = 10;
    filter_pts = 10.1;
    result = 1;
    int blit = 0;
    close_to(update_video(&blit), 0.1);
    assert(blit);
    /* Startup drops cannot turn the unknown timestamp sentinel into a PTS. */
    video.pts = video.last_pts = MP_NOPTS_VALUE;
    result = -1;
    close_to(update_video(&blit), video.frametime);
    assert(!blit && video.last_pts == MP_NOPTS_VALUE);
    result = 0;
    close_to(update_video(&blit), -1);
    return 0;
}
'''
            test = work / "test.c"
            test.write_text(program)
            binary = work / "timing"
            subprocess.run(["cc", "-fsanitize=undefined", str(test), "-lm", "-o", str(binary)], check=True)
            subprocess.run([str(binary)], check=True)

    def test_overlay_refresh_requests_a_flip_only_while_paused(self):
        with tempfile.TemporaryDirectory() as directory:
            work = pathlib.Path(directory)
            source = work / "command.c"
            shutil.copy(ROOT / "tools/testdata/mplayer-overlay-command.c", source)
            subprocess.run(["patch", str(source), str(ROOT / "docker/mplayer_overlay_refresh.patch")], check=True, capture_output=True)
            program = r'''
#include <assert.h>
#define MP_CMD_OSD_SHOW_TEXT 1
#define MP_CMD_OSD_SHOW_PROPERTY_TEXT 2
#define OSD_PAUSE 3
#define OSD_MSG_TEXT 4
static int osd_duration;
static void set_osd_msg(int id, int level, int duration, const char *format, const char *text) {}
typedef struct { int marker; } vf_instance_t;
typedef struct { vf_instance_t *vfilter; } video_t;
static struct { int osd_function; } context, *mpctx = &context;
static struct {
    struct { union { int i; const char *s; } v; } args[3];
} command, *cmd = &command;
static int flips;
/* Count dispatches to MPlayer's existing redraw path. This stub does not
 * exercise framebuffer presentation or the playback clock. */
static void vf_extra_flip(vf_instance_t *vf) {
    assert(vf && vf->marker == 42);
    flips++;
}
static void handle(video_t *sh_video) {
    switch (MP_CMD_OSD_SHOW_TEXT) {
#include "command.c"
    }
}
int main(void) {
    vf_instance_t vf = {42};
    video_t video = {&vf};
    cmd->args[0].v.s = " ";
    cmd->args[1].v.i = 1;
    for (int i = 0; i < 20; i++) handle(&video);
    assert(flips == 0);

    mpctx->osd_function = OSD_PAUSE;
    for (int i = 0; i < 20; i++) {
        handle(&video);
        assert(flips == i + 1);
        assert(mpctx->osd_function == OSD_PAUSE);
    }
    handle(0);
    video.vfilter = 0;
    handle(&video);
    assert(flips == 20);

    mpctx->osd_function = 0;
    video.vfilter = &vf;
    handle(&video);
    assert(flips == 20);
}
'''
            (work / "test.c").write_text(program)
            subprocess.run(["cc", "-fsanitize=undefined", str(work / "test.c"), "-o", str(work / "test")], check=True)
            subprocess.run([str(work / "test")], check=True)


if __name__ == "__main__":
    unittest.main()
