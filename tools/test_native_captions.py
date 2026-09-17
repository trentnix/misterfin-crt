"""Decode synthetic CC1 captions with the native adapter and real FFmpeg."""
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]


class NativeCaptionsTest(unittest.TestCase):
    def test_pop_on_and_clear(self):
        if not shutil.which("cc") or not shutil.which("pkg-config"):
            self.skipTest("C compiler and pkg-config required")
        flags = subprocess.run(["pkg-config", "--cflags", "--libs", "libavcodec", "libavutil"], capture_output=True, text=True)
        if flags.returncode:
            self.skipTest("FFmpeg development libraries required")
        with tempfile.TemporaryDirectory() as directory:
            source = Path(directory) / "captions.c"
            source.write_text(PROGRAM)
            binary = Path(directory) / "captions"
            subprocess.run(["cc", "-std=c11", "-Wall", "-Wextra", "-I", str(ROOT / "docker"), str(source), *flags.stdout.split(), "-o", str(binary)], check=True, capture_output=True)
            result = subprocess.run([str(binary)], check=True, capture_output=True, timeout=5)
            lines = result.stdout.splitlines()
            self.assertEqual(len(lines), 3)
            self.assertEqual(lines[0], b"ANS_CAPTION_ASS=")
            self.assertIn(b"HELLO", bytes.fromhex(lines[1].split(b"=")[1].decode()))
            self.assertEqual(lines[2], b"ANS_CAPTION_ASS=")


PROGRAM = r'''
#include "mistervision_captions.h"
static unsigned char parity(unsigned char b) { return b | ((__builtin_parity(b) ^ 1) << 7); }
static void packet(struct mf_captions *s, const unsigned char *pairs, int n) {
 AVFrame *f = av_frame_alloc();
 AVFrameSideData *side = av_frame_new_side_data(f, AV_FRAME_DATA_A53_CC, n*3);
 for(int i=0;i<n;i++){side->data[i*3]=0xfc;side->data[i*3+1]=parity(pairs[i*2]);side->data[i*3+2]=parity(pairs[i*2+1]);}
 mf_caption_frame(s,f);av_frame_free(&f);
}
int main(void) {
 struct mf_captions s={0};
 unsigned char show[]={0x14,0x20,0x14,0x2e,0x14,0x60,'H','E','L','L','O',' ',0x14,0x2f};
 unsigned char clear[]={0x14,0x2c};
 packet(&s,show,sizeof(show)/2);packet(&s,clear,1);
 avcodec_free_context(&s.decoder);
}
'''
