//go:build linux

// Package terminal provides cancellable desktop keyboard input without cgo.
package terminal

import (
	"context"
	"syscall"
	"time"
	"unsafe"
)

// Read temporarily disables line buffering and echo. Signal keys keep their
// normal meaning. The goroutine restores the terminal before closing done.
func Read(ctx context.Context) (<-chan string, <-chan struct{}, error) {
	fd, err := syscall.Open("/dev/tty", syscall.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, nil, err
	}
	var old syscall.Termios
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), syscall.TCGETS, uintptr(unsafe.Pointer(&old))); e != 0 {
		syscall.Close(fd)
		return nil, nil, e
	}
	next := old
	next.Lflag &^= syscall.ICANON | syscall.ECHO
	next.Cc[syscall.VMIN] = 0
	next.Cc[syscall.VTIME] = 0
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), syscall.TCSETS, uintptr(unsafe.Pointer(&next))); e != 0 {
		syscall.Close(fd)
		return nil, nil, e
	}
	out := make(chan string, 32)
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer close(out)
		defer syscall.Close(fd)
		defer syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), syscall.TCSETS, uintptr(unsafe.Pointer(&old)))
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		var decoder Decoder
		buf := make([]byte, 128)
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				n, e := syscall.Read(fd, buf)
				if e != nil && e != syscall.EAGAIN && e != syscall.EINTR {
					return
				}
				if n < 0 {
					n = 0
				}
				for _, key := range decoder.Feed(buf[:n], now) {
					select {
					case out <- key:
					case <-ctx.Done():
						return
					}
				}
			}
		}
	}()
	return out, done, nil
}
