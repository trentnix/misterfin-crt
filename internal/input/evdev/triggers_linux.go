//go:build linux

package evdev

import (
	"syscall"
	"unsafe"

	"misterfin-crt/internal/input/control"
)

// triggerAxis converts an analog trigger into a held action. Separate press
// and release thresholds prevent noise near the threshold from issuing seeks.
type triggerAxis struct {
	min, max int32
	pressed  bool
}

func (a *triggerAxis) action(value int32, key control.Action) control.Action {
	level := int64(value) - int64(a.min)
	rangeSize := int64(a.max) - int64(a.min)
	threshold := int64(25)
	if a.pressed {
		threshold = 15
	}
	a.pressed = rangeSize > 0 && level*100 >= rangeSize*threshold
	if a.pressed {
		return key
	}
	return ""
}

func triggerKey(code uint16) control.Action {
	switch code {
	case 2, 10: // ABS_Z, ABS_BRAKE
		return control.SeekBackward
	case 5, 9: // ABS_RZ, ABS_GAS
		return control.SeekForward
	}
	return ""
}

// discoverTriggers uses advertised ranges on gamepads with shoulder buttons.
// It excludes sticks and unrelated absolute devices such as touchscreens.
func discoverTriggers(fd int) map[uint16]*triggerAxis {
	var keys [96]byte
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), 0x80604521, uintptr(unsafe.Pointer(&keys[0]))) // EVIOCGBIT(EV_KEY)
	if errno != 0 || keys[310/8]&(1<<(310%8)) == 0 || keys[311/8]&(1<<(311%8)) == 0 {
		return nil
	}
	axes := make(map[uint16]*triggerAxis)
	for _, code := range []uint16{2, 5, 9, 10} {
		if min, max, ok := axisRange(fd, code); ok {
			axes[code] = &triggerAxis{min: min, max: max}
		}
	}
	return axes
}
