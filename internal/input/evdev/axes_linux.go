//go:build linux

package evdev

import (
	"syscall"
	"unsafe"

	"misterfin-crt/internal/input/control"
)

// mappedAxis translates a configured axis into held actions. Each device owns
// its state, so disconnecting one controller cannot leave another held down.
type mappedAxis struct {
	binding        Axis
	min, rest, max int64
	held           control.Action
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

func (a *mappedAxis) action(value int32) control.Action {
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

// axisBindings adds gamepad left-stick defaults before applying explicit
// bindings. Keep defaults out of Profile so labels can prefer configured inputs.
func (d *device) axisBindings(keys [96]byte) map[uint16]Axis {
	bindings := make(map[uint16]Axis)
	// BTN_GAMEPAD (BTN_SOUTH) identifies a gamepad rather than a touchscreen
	// or pointer with ABS_X/ABS_Y. MiSTer's virtual node supplies arrows only.
	if !d.bindings.Replace && d.name != "MiSTer virtual input" && keys[304/8]&(1<<(304%8)) != 0 {
		bindings[0] = Axis{Negative: control.Previous, Positive: control.Next, Press: 40, Release: 25}
		bindings[1] = Axis{Negative: control.Up, Positive: control.Down, Press: 40, Release: 25}
	}
	for code, binding := range d.bindings.Axes {
		bindings[code] = binding
	}
	return bindings
}

func (d *device) configure(config Config) {
	d.bindings = config.bindings(d.name)
	keys := keyCapabilities(d.fd)
	_, _, d.hats[0] = axisRange(d.fd, 16)
	_, _, d.hats[1] = axisRange(d.fd, 17)
	d.axes = make(map[uint16]*mappedAxis)
	for code, binding := range d.axisBindings(keys) {
		if min, max, ok := axisRange(d.fd, code); ok {
			d.axes[code] = newMappedAxis(binding, min, max)
		}
	}
	d.legend = d.labels(keys)
}

func (d *device) mappedAction(e event) control.Action {
	if e.Type == 1 {
		if e.Value != 1 {
			return ""
		}
		if key, ok := d.bindings.Buttons[e.Code]; ok {
			return key
		}
	} else if axis := d.axes[e.Code]; axis != nil {
		return axis.action(e.Value)
	} else if _, configured := d.bindings.Axes[e.Code]; configured {
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
