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

/* MPlayer 1.5 update_video correct_pts branch, for patch regression tests. */
static double update_video(int *blit_frame)
{
    sh_video_t *sh_video = mpctx->sh_video;
    double frame_time;
    *blit_frame = 0;
        int res = generate_video_frame(sh_video, mpctx->d_video);
        if (!res)
            return -1;
        ((vf_instance_t *)sh_video->vfilter)->control(sh_video->vfilter,
                                                      VFCTRL_GET_PTS, &sh_video->pts);
        ((vf_instance_t *)sh_video->vfilter)->control(sh_video->vfilter,
                                                      VFCTRL_GET_ENDPTS, &sh_video->endpts);
        if (sh_video->pts == MP_NOPTS_VALUE) {
            mp_msg(MSGT_CPLAYER, MSGL_ERR, MSGTR_PtsAfterFiltersMissing);
            sh_video->pts = sh_video->last_pts;
        }
        if (sh_video->last_pts == MP_NOPTS_VALUE)
            sh_video->last_pts = sh_video->pts;
        else if (sh_video->last_pts > sh_video->pts) {
            // make a guess whether this is some kind of discontinuity
            // we should jump along with or some wrong timestamps we
            // should replace instead
            if (sh_video->pts < sh_video->last_pts - 20 * sh_video->frametime)
                sh_video->last_pts = sh_video->pts;
            else
                sh_video->pts = sh_video->last_pts + sh_video->frametime;
            mp_msg(MSGT_CPLAYER, MSGL_V, "pts value < previous\n");
        }
        frame_time = sh_video->pts - sh_video->last_pts;
        // The first frame should be displayed directly,
        // all others should get a default frame_time
        if (!frame_time && mpctx->startup_decode_retry == 0)
            frame_time = sh_video->frametime;
        sh_video->last_pts = sh_video->pts;
        advance_timer(frame_time);
        *blit_frame = res > 0;
    return frame_time;
}
