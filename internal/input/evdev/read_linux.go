//go:build linux

// Package evdev reads MiSTer controllers and keyboards directly. Button mapping
// and virtual-device filtering follow the preserved C client's src/input.c.
package evdev

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

type event struct {
	Time       syscall.Timeval
	Type, Code uint16
	Value      int32
}

type device struct {
	fd   int
	name string
	held map[uint16]string
}

// action ignores MiSTer's synthetic action keys. Its arrow echoes are needed
// by pads whose directional input is available only through that virtual node.
func action(name string, kind, code uint16, value int32) string {
	virtual := name == "MiSTer virtual input"
	if kind == 3 {
		if virtual {
			return ""
		}
		switch code {
		case 16:
			if value < 0 {
				return "previous"
			}
			if value > 0 {
				return "next"
			}
		case 17:
			if value < 0 {
				return "up"
			}
			if value > 0 {
				return "down"
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
		return "up"
	case 108:
		return "down"
	case 105, 310, 104:
		return "previous"
	case 106, 311, 109:
		return "next"
	case 305, 28, 45, 48:
		return "open" // BTN_EAST, Enter, X, as in C
	case 304, 1, 158, 14, 44, 30:
		return "back" // BTN_SOUTH, Escape, Back, Backspace, Z
	case 314, 15:
		return "select" // BTN_SELECT (Xbox View/Back), Tab. Y is unmapped.
	case 19:
		return "retry"
	case 16:
		return "quit" // Q on a physical keyboard
	}
	return ""
}

func (d *device) accept(e event) string {
	if e.Type != 1 && e.Type != 3 {
		return ""
	}
	// Namespace axes separately from key codes in the held-state table.
	code := e.Code
	if e.Type == 3 {
		code |= 0x8000
	}
	key := action(d.name, e.Type, e.Code, e.Value)
	if e.Type == 1 && e.Value == 2 {
		return ""
	} // use our own navigation repeat
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
	repeats map[string]navigationRepeat
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

func (n *navigation) update(held, pressed map[string]bool, now time.Time) []string {
	if n.repeats == nil {
		n.repeats = make(map[string]navigationRepeat)
	}
	var keys []string
	for _, key := range []string{"up", "down", "previous", "next"} {
		repeat, active := n.repeats[key]
		if !active && (held[key] || pressed[key]) {
			keys = append(keys, key)
			n.repeats[key] = navigationRepeat{next: now.Add(repeatDelay)}
		} else if held[key] && !now.Before(repeat.next) {
			keys = append(keys, key+"-repeat")
			interval := repeatSlow
			if repeat.count >= repeatRampAfter {
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

func direction(key string) bool {
	return key == "up" || key == "down" || key == "previous" || key == "next"
}

func openDevices(devices map[string]*device) {
	paths, _ := filepath.Glob("/dev/input/event*")
	for _, path := range paths {
		if devices[path] != nil {
			continue
		}
		fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
		if err != nil {
			continue
		}
		var name [128]byte
		// EVIOCGNAME(sizeof(name)), from linux/input.h. No exclusive grab.
		_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), 0x80804506, uintptr(unsafe.Pointer(&name[0])))
		if errno != 0 {
			syscall.Close(fd)
			continue
		}
		devices[path] = &device{fd: fd, name: strings.TrimRight(string(name[:]), "\x00"), held: make(map[uint16]string)}
	}
}

// Read owns all event descriptors and rescans for hotplugged controllers.
// The terminal is not read here, so virtual joystick echoes cannot fire twice.
func Read(ctx context.Context) (<-chan string, <-chan struct{}, error) {
	devices := make(map[string]*device)
	openDevices(devices)
	if len(devices) == 0 {
		return nil, nil, errors.New("cannot open hardware input devices")
	}
	out := make(chan string, 32)
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
		send := func(key string) bool {
			if key == "" {
				return true
			}
			select {
			case out <- key:
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
					openDevices(devices)
					scan = now.Add(2 * time.Second)
				}
				pressed := make(map[string]bool)
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
						if direction(key) {
							pressed[key] = true
						} else if key != "" {
							if !send(key) {
								return
							}
						}
					}
				}
				held := make(map[string]bool)
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
