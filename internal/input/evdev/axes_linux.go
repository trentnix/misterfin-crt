//go:build linux

package evdev

import (
	"syscall"
	"unsafe"
)

// mappedAxis translates a configured axis into held actions. Each device owns
// its state, so disconnecting one controller cannot leave another held down.
type mappedAxis struct {
	binding        Axis
	min, rest, max int64
	held           string
}

func newMappedAxis(binding Axis, min, max int32) *mappedAxis {
	a := &mappedAxis{binding: binding, min: int64(min), max: int64(max)}
	a.rest = a.min + (a.max-a.min)/2
	switch binding.Rest {
	case "minimum":
		a.rest = a.min
	case "maximum":
		a.rest = a.max
	}
	return a
}

func (a *mappedAxis) action(value int32) string {
	key, travel, span := a.binding.Positive, int64(value)-a.rest, a.max-a.rest
	if int64(value) < a.rest {
		key, travel, span = a.binding.Negative, a.rest-int64(value), a.rest-a.min
	}
	press, release := a.binding.thresholds()
	threshold := press
	if a.held == key {
		threshold = release
	}
	a.held = ""
	if key != "" && span > 0 && travel*100 >= span*int64(threshold) {
		a.held = key
	}
	return a.held
}

func axisRange(fd int, code uint16) (min, max int32, ok bool) {
	var info struct{ Value, Minimum, Maximum, Fuzz, Flat, Resolution int32 }
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(0x80184540)+uintptr(code), uintptr(unsafe.Pointer(&info))) // EVIOCGABS
	return info.Minimum, info.Maximum, errno == 0 && info.Maximum > info.Minimum
}

func (d *device) configure(config Config) {
	d.bindings = config.bindings(d.name)
	d.axes = make(map[uint16]*mappedAxis)
	for code, binding := range d.bindings.Axes {
		if min, max, ok := axisRange(d.fd, code); ok {
			d.axes[code] = newMappedAxis(binding, min, max)
		}
	}
}

func (d *device) mappedAction(e event) string {
	if e.Type == 1 {
		if e.Value != 1 {
			return ""
		}
		if key, ok := d.bindings.Buttons[e.Code]; ok {
			return key
		}
	} else if _, ok := d.bindings.Axes[e.Code]; ok {
		if axis := d.axes[e.Code]; axis != nil {
			return axis.action(e.Value)
		}
		return ""
	}
	if d.bindings.Replace {
		return ""
	}
	if e.Type == 3 && d.triggers[e.Code] != nil {
		return d.triggers[e.Code].action(e.Value, triggerKey(e.Code))
	}
	return action(d.name, e.Type, e.Code, e.Value)
}
