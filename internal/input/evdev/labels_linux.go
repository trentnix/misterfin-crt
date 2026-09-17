//go:build linux

package evdev

import (
	"fmt"
	"syscall"
	"unsafe"

	"mistervision/internal/input/control"
)

func keyCapabilities(fd int) [96]byte {
	var keys [96]byte
	syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), 0x80604521, uintptr(unsafe.Pointer(&keys[0]))) // EVIOCGBIT(EV_KEY)
	return keys
}

// labels resolves the same effective bindings used by mappedAction. Only
// advertised buttons and discovered axes qualify. Explicit bindings take
// precedence over inherited aliases, with stable code order as the tie breaker.
func (d *device) labels(keys [96]byte) control.Labels {
	labels := make(control.Labels)
	for _, explicit := range []bool{true, false} {
		for code := uint16(0); code < 768; code++ {
			if keys[code/8]&(1<<(code%8)) == 0 {
				continue
			}
			key, configured := d.bindings.Buttons[code]
			if configured != explicit {
				continue
			}
			if !configured {
				if d.bindings.Replace {
					continue
				}
				key = action(d.name, 1, code, 1)
			}
			if key != "" && labels[key] == "" {
				label := d.bindings.ButtonLabels[code]
				if label == "" {
					label = buttonLabel(code)
				}
				labels[key] = label
			}
		}
		for code := uint16(0); code < 64; code++ {
			binding, configured := d.bindings.Axes[code]
			if configured != explicit {
				continue
			}
			if configured {
				if d.axes[code] == nil {
					continue
				}
			} else {
				if d.bindings.Replace {
					continue
				}
				if axis := d.axes[code]; axis != nil {
					binding = axis.binding
				} else if d.triggers[code] != nil {
					binding = Axis{Positive: triggerKey(code)}
				} else if code == 16 && d.hats[0] {
					binding = Axis{Negative: control.Previous, Positive: control.Next}
				} else if code == 17 && d.hats[1] {
					binding = Axis{Negative: control.Up, Positive: control.Down}
				} else {
					continue
				}
			}
			names := d.bindings.AxisLabels[code]
			for _, side := range []struct {
				key      control.Action
				label    string
				positive bool
			}{{binding.Negative, names.Negative, false}, {binding.Positive, names.Positive, true}} {
				if side.key == "" || labels[side.key] != "" {
					continue
				}
				label := side.label
				if label == "" {
					label = axisLabel(code, side.positive)
				}
				labels[side.key] = label
			}
		}
	}
	return labels
}

func buttonLabel(code uint16) string {
	if label := map[uint16]string{59: "F1", 1: "Esc", 14: "Backspace", 15: "Tab", 16: "Q", 19: "R", 26: "[", 27: "]", 28: "Enter", 30: "A", 36: "J", 38: "L", 44: "Z", 45: "X", 48: "B", 103: "Up", 104: "PgUp", 105: "Left", 106: "Right", 108: "Down", 109: "PgDn", 158: "Back", 304: "A", 305: "B", 307: "X", 308: "Y", 310: "LB", 311: "RB", 312: "LT", 313: "RT", 314: "Select", 315: "Start", 317: "LS", 318: "RS"}[code]; label != "" {
		return label
	}
	return fmt.Sprintf("Btn %d", code)
}

func axisLabel(code uint16, positive bool) string {
	switch code {
	case 2, 10:
		return "LT"
	case 5, 9:
		return "RT"
	case 0, 16:
		if positive {
			return "Right"
		}
		return "Left"
	case 1, 17:
		if positive {
			return "Down"
		}
		return "Up"
	}
	sign := "-"
	if positive {
		sign = "+"
	}
	return fmt.Sprintf("Axis %d%s", code, sign)
}
