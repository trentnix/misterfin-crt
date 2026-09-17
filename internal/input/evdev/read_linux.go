//go:build linux

// Package evdev reads MiSTer controllers and keyboards directly, applying
// configurable bindings and filtering duplicate virtual-device events.
package evdev

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"mistervision/internal/diagnostics"
	"mistervision/internal/input/control"
)

type event struct {
	Time       syscall.Timeval
	Type, Code uint16
	Value      int32
}

type device struct {
	fd       int
	name     string
	held     map[uint16]control.Action
	triggers map[uint16]*triggerAxis
	bindings Profile
	axes     map[uint16]*mappedAxis
	hats     [2]bool
	legend   control.Labels
}

// action ignores MiSTer's synthetic action keys. Its arrow echoes are needed
// by pads whose directional input is available only through that virtual node.
func action(name string, kind, code uint16, value int32) control.Action {
	virtual := name == "MiSTer virtual input"
	if kind == 3 {
		if virtual {
			return ""
		}
		switch code {
		case 16:
			if value < 0 {
				return control.Previous
			}
			if value > 0 {
				return control.Next
			}
		case 17:
			if value < 0 {
				return control.Up
			}
			if value > 0 {
				return control.Down
			}
		}
		return ""
	}
	if kind != 1 || value != 1 {
		return ""
	}
	if virtual && code != 103 && code != 108 && code != 105 && code != 106 {
		return ""
	}
	if strings.Contains(name, "SFC30") {
		if code == 304 {
			code = 305
		} else if code == 305 {
			code = 304
		}
	}
	switch code {
	case 103:
		return control.Up
	case 108:
		return control.Down
	case 105:
		return control.Previous
	case 106:
		return control.Next
	case 310, 104, 26:
		return control.TrackPrevious // LB, Page Up, [
	case 311, 109, 27:
		return control.TrackNext // RB, Page Down, ]
	case 312, 36:
		return control.SeekBackward // Digital LT, J
	case 313, 38:
		return control.SeekForward // Digital RT, L
	case 304, 28, 45, 48:
		return control.Open // Xbox A (BTN_SOUTH), Enter, X, B
	case 305, 1, 158, 14, 44, 30:
		return control.Back // Xbox B (BTN_EAST), Escape, Back, Backspace, Z, A
	case 315, 59:
		return control.About // BTN_START (Xbox Menu), F1
	case 314, 15:
		return control.Select // BTN_SELECT (Xbox View/Back), Tab. Y is unmapped.
	case 19:
		return control.Retry
	case 16:
		return control.Quit // Q on a physical keyboard
	}
	return ""
}

func (d *device) accept(e event) control.Action {
	if e.Type != 1 && e.Type != 3 {
		return ""
	}
	// Namespace axes separately from key codes in the held-state table.
	code := e.Code
	if e.Type == 3 {
		code |= 0x8000
	}
	if e.Type == 1 && e.Value == 2 {
		return ""
	} // use our own navigation repeat
	key := d.mappedAction(e)
	// Never re-enable synthetic action echoes through a broad device profile.
	if d.name == "MiSTer virtual input" && (e.Type != 1 || (e.Code != 103 && e.Code != 108 && e.Code != 105 && e.Code != 106)) {
		key = ""
	}
	if key == "" {
		delete(d.held, code)
		return ""
	}
	if d.held[code] == key {
		return ""
	}
	d.held[code] = key
	return key
}

// navigation merges physical directions and MiSTer's virtual arrow echoes.
// One held direction produces one press and one repeat stream across devices.
type navigation struct {
	repeats map[control.Action]navigationRepeat
}

// navigationRepeat follows the C client's two-stage hold timing. Scheduling
// from the current poll avoids a burst of queued repeats after a slow frame.
type navigationRepeat struct {
	next  time.Time
	count int
}

const (
	repeatDelay     = 350 * time.Millisecond
	repeatSlow      = 110 * time.Millisecond
	repeatFast      = 45 * time.Millisecond
	repeatRampAfter = 6
)

func (n *navigation) update(held, pressed map[control.Action]bool, now time.Time) []control.Action {
	if n.repeats == nil {
		n.repeats = make(map[control.Action]navigationRepeat)
	}
	var keys []control.Action
	for _, key := range []control.Action{control.Up, control.Down, control.Previous, control.Next, control.TrackPrevious, control.TrackNext, control.SeekBackward, control.SeekForward} {
		repeat, active := n.repeats[key]
		if !active && (held[key] || pressed[key]) {
			keys = append(keys, key)
			n.repeats[key] = navigationRepeat{next: now.Add(repeatDelay)}
		} else if held[key] && !now.Before(repeat.next) {
			keys = append(keys, key.Repeat())
			interval := repeatSlow
			if key == control.SeekBackward || key == control.SeekForward {
				interval = 250 * time.Millisecond
			} else if repeat.count >= repeatRampAfter {
				interval = repeatFast
			} else {
				repeat.count++
			}
			repeat.next = now.Add(interval)
			n.repeats[key] = repeat
		}
		if !held[key] {
			delete(n.repeats, key)
		}
	}
	return keys
}

func direction(key control.Action) bool {
	return key == control.Up || key == control.Down || key == control.Previous || key == control.Next || key == control.TrackPrevious || key == control.TrackNext || key == control.SeekBackward || key == control.SeekForward
}

// openDevices adds newly available nodes without grabbing them exclusively.
// The optional log describes only this scan and never records input events.
func openDevices(devices map[string]*device, config Config, log *diagnostics.Log) {
	paths, _ := filepath.Glob("/dev/input/event*")
	for _, path := range paths {
		if devices[path] != nil {
			continue
		}
		fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
		if err != nil {
			log.Record("input.unavailable", slog.String("node", filepath.Base(path)), slog.String("stage", "open"), slog.String("error_kind", diagnostics.ErrorKind(err)))
			continue
		}
		var name [128]byte
		// EVIOCGNAME(sizeof(name)), from linux/input.h. No exclusive grab.
		_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), 0x80804506, uintptr(unsafe.Pointer(&name[0])))
		if errno != 0 {
			log.Record("input.unavailable", slog.String("node", filepath.Base(path)), slog.String("stage", "identify"), slog.String("error_kind", diagnostics.ErrorKind(errno)))
			syscall.Close(fd)
			continue
		}
		devices[path] = &device{fd: fd, name: strings.TrimRight(string(name[:]), "\x00"), held: make(map[uint16]control.Action), triggers: discoverTriggers(fd)}
		devices[path].configure(config)
		if log != nil {
			d := devices[path]
			name := strings.Map(func(r rune) rune {
				if r < 32 || r == 127 {
					return -1
				}
				return r
			}, strings.ToValidUTF8(d.name, "?"))
			log.Record("input.device", slog.String("node", filepath.Base(path)), slog.String("name", name), slog.Bool("virtual", d.name == "MiSTer virtual input"), slog.Bool("replace_bindings", d.bindings.Replace), slog.Int("mapped_buttons", len(d.bindings.Buttons)), slog.Int("mapped_axes", len(d.axes)), slog.Int("triggers", len(d.triggers)))
		}
	}
}

// Read owns all event descriptors and rescans for hotplugged controllers.
// A non-nil log records the initial device scan, never input events or hotplug polls.
// The terminal is not read here, so virtual joystick echoes cannot fire twice.
func Read(ctx context.Context, config Config, log *diagnostics.Log) (<-chan control.Event, <-chan struct{}, error) {
	if err := config.Validate(); err != nil {
		return nil, nil, err
	}
	devices := make(map[string]*device)
	openDevices(devices, config, log)
	if len(devices) == 0 {
		return nil, nil, errors.New("cannot open hardware input devices")
	}
	out := make(chan control.Event, 32)
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer close(out)
		defer func() {
			for _, d := range devices {
				syscall.Close(d.fd)
			}
		}()
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		var nav navigation
		scan := time.Now().Add(2 * time.Second)
		var active control.Labels
		send := func(key control.Action) bool {
			if key == "" {
				return true
			}
			select {
			case out <- control.Event{Action: key, Labels: active}:
				return true
			case <-ctx.Done():
				return false
			}
		}
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				if !now.Before(scan) {
					openDevices(devices, config, nil)
					scan = now.Add(2 * time.Second)
				}
				pressed := make(map[control.Action]bool)
				for path, d := range devices {
					for {
						var e event
						data := unsafe.Slice((*byte)(unsafe.Pointer(&e)), int(unsafe.Sizeof(e)))
						n, err := syscall.Read(d.fd, data)
						if err == syscall.EAGAIN || err == syscall.EINTR {
							break
						}
						if err != nil || n != len(data) {
							syscall.Close(d.fd)
							delete(devices, path)
							break
						}
						key := d.accept(e)
						if key != "" && d.name != "MiSTer virtual input" {
							active = d.legend
						}
						if direction(key) {
							pressed[key] = true
						} else if key != "" {
							if !send(key) {
								return
							}
						}
					}
				}
				held := make(map[control.Action]bool)
				for _, d := range devices {
					for _, key := range d.held {
						held[key] = true
					}
				}
				for _, key := range nav.update(held, pressed, now) {
					if !send(key) {
						return
					}
				}
			}
		}
	}()
	return out, done, nil
}
