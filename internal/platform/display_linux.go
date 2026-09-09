//go:build linux && cgo

package platform

/*
#cgo CFLAGS: -std=c11 -D_GNU_SOURCE -Wall -Wextra
#include "adapter.h"
#include <stdlib.h>
*/
import "C"

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

type framebuffer struct {
	handle   *C.mf_display
	geometry Geometry
	output   string
}

func Open(options Options) (Display, error) {
	var w, h int
	if options.Headless != "" {
		parts := strings.Split(options.Headless, "x")
		if len(parts) == 2 {
			w, _ = strconv.Atoi(parts[0])
			h, _ = strconv.Atoi(parts[1])
		}
		if w < 1 || w > 8192 || h < 1 || h > 8192 {
			return nil, errors.New("headless geometry must be WxH with dimensions from 1 to 8192")
		}
	} else if options.Output != "" {
		return nil, errors.New("frame output requires headless mode")
	}
	if strings.ContainsRune(options.Device, 0) || strings.ContainsRune(options.Output, 0) {
		return nil, errors.New("paths must not contain NUL")
	}
	if options.Device == "" {
		options.Device = "/dev/fb0"
	}
	device := C.CString(options.Device)
	defer C.free(unsafe.Pointer(device))
	d := &framebuffer{output: options.Output}
	if err := status("open framebuffer", C.mf_open(&d.handle, device, C.int(w), C.int(h))); err != nil {
		return nil, err
	}
	var cw, ch, ow, oh C.int
	C.mf_geometry(d.handle, &cw, &ch, &ow, &oh)
	d.geometry = Geometry{int(cw), int(ch), int(ow), int(oh)}
	return d, nil
}

func (d *framebuffer) Geometry() Geometry { return d.geometry }

func (d *framebuffer) Present(pixels []byte) error {
	if d.handle == nil {
		return errors.New("framebuffer is closed")
	}
	if len(pixels) != d.geometry.Width*d.geometry.Height*4 {
		return errors.New("frame must contain exactly width * height * 4 BGRX bytes")
	}
	if err := status("present framebuffer", C.mf_present(d.handle, (*C.uint8_t)(unsafe.Pointer(&pixels[0])), C.size_t(len(pixels)))); err != nil {
		return err
	}
	if d.output != "" {
		path := C.CString(d.output)
		defer C.free(unsafe.Pointer(path))
		return status("dump framebuffer", C.mf_dump(d.handle, path))
	}
	return nil
}

func (d *framebuffer) Close() error {
	err := status("close framebuffer", C.mf_close(d.handle))
	d.handle = nil
	return err
}

func status(operation string, code C.int) error {
	if code == 0 {
		return nil
	}
	return fmt.Errorf("%s: %w", operation, syscall.Errno(code))
}
