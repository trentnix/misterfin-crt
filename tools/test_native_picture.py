"""Exercise the native picture filter's geometry, cache, and redraw contract."""
import pathlib
import re
import shutil
import subprocess
import tempfile
import unittest

ROOT = pathlib.Path(__file__).resolve().parents[1]


@unittest.skipUnless(shutil.which("cc"), "C compiler required")
class NativePictureTest(unittest.TestCase):
    def test_cached_frame_toggles_without_reconfiguring_output(self):
        # The filter is production code. Small MPlayer/scaler doubles expose its
        # crop/fit geometry, source ownership, timestamps, and output lifetime.
        source = (ROOT / "docker/vf_misterfin.c").read_text()
        source = re.sub(r'^#include ".*"\n', '', source, flags=re.M)
        prefix = r'''
#include <assert.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#define IMGFMT_YV12 1
#define IMGFMT_I420 2
#define IMGFMT_IYUV 3
#define IMGFMT_BGR32 4
#define MP_IMGTYPE_TEMP 1
#define MP_IMGFLAG_ACCEPT_STRIDE 1
#define MP_IMGFLAG_PREFER_ALIGNED_STRIDE 2
#define VFCAP_CSP_SUPPORTED_BY_HW 4
#define VFCTRL_MISTERFIN_PICTURE 100
#define CONTROL_TRUE 1
#define CONTROL_FALSE 0
#define SWS_FAST_BILINEAR 1
#define imgfmt2pixfmt(f) (f)
typedef struct mp_image {
 int w,h,stride[4],imgfmt;
 uint8_t *planes[4];
} mp_image_t;
struct vf_priv_s;
typedef struct vf_instance vf_instance_t;
struct vf_instance {
 struct vf_priv_s *priv;
 vf_instance_t *next;
 int (*config)(vf_instance_t *,int,int,int,int,unsigned int,unsigned int);
 int (*put_image)(vf_instance_t *,mp_image_t *,double,double);
 int (*control)(vf_instance_t *,int,void *);
 int (*query_format)(vf_instance_t *,unsigned int);
 void (*uninit)(vf_instance_t *);
};
typedef struct vf_info {
 const char *info,*name,*author,*comment;
 int (*open)(vf_instance_t *,char *);
 void *options;
} vf_info_t;
struct SwsContext { int sw,sh,dw,dh; };
static int configured, flips, allocations;
static double last_pts;
static mp_image_t output;
static struct SwsContext *sws_getCachedContext(struct SwsContext *c,int sw,int sh,int sf,int dw,int dh,int df,int flags,void *a,void *b,void *d) {
 if (!c) { c=calloc(1,sizeof(*c)); allocations++; }
 c->sw=sw;c->sh=sh;c->dw=dw;c->dh=dh;return c;
}
static void sws_freeContext(struct SwsContext *c) { free(c); }
static int sws_scale(struct SwsContext *c,const uint8_t *const src[],const int stride[],int y,int h,uint8_t *const dst[],const int ds[]) {
 for(int yy=0;yy<c->dh;yy++) for(int x=0;x<c->dw;x++) {
  uint8_t value=src[0][yy*c->sh/c->dh*stride[0]+x*c->sw/c->dw];
  memset(dst[0]+yy*ds[0]+x*4,value,4);
 }
 return c->dh;
}
static mp_image_t *alloc_mpi(int w,int h,unsigned long fmt) {
 mp_image_t *p=calloc(1,sizeof(*p));p->w=w;p->h=h;p->imgfmt=fmt;
 for(int i=0;i<3;i++) { int shift=i!=0;p->stride[i]=w>>shift;p->planes[i]=calloc(h>>shift,p->stride[i]); }
 return p;
}
static void free_mp_image(mp_image_t *p) { if(p){for(int i=0;i<3;i++)free(p->planes[i]);free(p);} }
static void memcpy_pic(uint8_t *d,const uint8_t *s,int w,int h,int ds,int ss) { for(int y=0;y<h;y++)memcpy(d+y*ds,s+y*ss,w); }
static mp_image_t *vf_get_image(vf_instance_t *v,unsigned int fmt,int type,int flags,int w,int h) {
 if(!output.planes[0])output.planes[0]=malloc(640*576*4);
 output.w=w;output.h=h;output.stride[0]=w*4;return &output;
}
static int vf_next_put_image(vf_instance_t *v,mp_image_t *p,double pts,double endpts) {last_pts=pts;return 1;}
static int vf_next_config(vf_instance_t *v,int w,int h,int dw,int dh,unsigned int flags,unsigned int fmt) {configured++;return 1;}
static int vf_next_query_format(vf_instance_t *v,unsigned int f) {return 1;}
static int vf_next_control(vf_instance_t *v,int req,void *p) {return 0;}
static void vf_extra_flip(vf_instance_t *v) {flips++;}
'''
        suffix = r'''
int main(void) {
 for(int height=240;height<=576;height+=48) {
  if(height!=240&&height!=288&&height!=480&&height!=576)continue;
  vf_instance_t vf={0};char args[80];
  snprintf(args,sizeof(args),"640:%d:1.777777778:0",height);
  assert(vf_open(&vf,args));
  configured=flips=allocations=0;
  assert(vf.config(&vf,720,576,1024,576,0,IMGFMT_YV12));
  // Visible width is smaller than the decoder's allocated row stride.
  mp_image_t *input=alloc_mpi(736,576,IMGFMT_YV12);input->w=720;
  for(int y=0;y<576;y++)memset(input->planes[0]+y*736+90,200,540);
  assert(vf.put_image(&vf,input,12.5,12.54));
  size_t bytes=640*height*4;
  uint8_t *original=malloc(bytes);memcpy(original,output.planes[0],bytes);
  assert(output.planes[0][(height/2*640+16)*4]==0);
  assert(output.planes[0][(height/16*640+320)*4]==0);
  // The next decoder write must not change a paused comparison frame.
  memset(input->planes[0],77,736*576);
  for(int n=0;n<100;n++) {
   int mode=1;
   assert(vf.control(&vf,VFCTRL_MISTERFIN_PICTURE,&mode)==CONTROL_TRUE);
   assert(output.planes[0][(height/2*640+16)*4]==200);
   assert(output.planes[0][(height/16*640+320)*4]==200);
   mode=0;
   assert(vf.control(&vf,VFCTRL_MISTERFIN_PICTURE,&mode)==CONTROL_TRUE);
   assert(memcmp(original,output.planes[0],bytes)==0);
   assert(last_pts==12.5 && configured==1);
  }
  assert(allocations==2 && flips==200);
  int invalid=2;
  assert(vf.control(&vf,VFCTRL_MISTERFIN_PICTURE,&invalid)==CONTROL_FALSE);
  assert(memcmp(original,output.planes[0],bytes)==0);
  free(original);free_mp_image(input);vf.uninit(&vf);
 }
 {
  vf_instance_t vf={0};
  char args[]="640:576:1.333333333:0";
  assert(vf_open(&vf,args));
  assert(vf.config(&vf,720,576,720,576,0,IMGFMT_YV12));
  mp_image_t *input=alloc_mpi(720,576,IMGFMT_YV12);
  for(int y=72;y<504;y++)memset(input->planes[0]+y*720+90,200,540);
  assert(vf.put_image(&vf,input,20.0,20.04));
  assert(output.planes[0][0]==0);
  int mode=1;
  assert(vf.control(&vf,VFCTRL_MISTERFIN_PICTURE,&mode)==CONTROL_TRUE);
  // A 4:3 encoded frame receives a centered 4/3 enlargement.
  assert(vf.priv->scaler[1]->sw==540);
  assert(vf.priv->scaler[1]->sh==432);
  assert(output.planes[0][0]==200);
  free_mp_image(input);
  vf.uninit(&vf);
 }
 free(output.planes[0]);
 return 0;
}
'''
        with tempfile.TemporaryDirectory() as directory:
            work = pathlib.Path(directory)
            c = work / "picture.c"
            binary = work / "picture"
            c.write_text(prefix + source + suffix)
            subprocess.run(["cc", "-std=c99", "-O2", "-Wall", "-Wextra", "-Wno-unused-parameter", str(c), "-o", str(binary)], check=True, capture_output=True)
            subprocess.run([str(binary)], check=True, capture_output=True)


if __name__ == "__main__":
    unittest.main()
